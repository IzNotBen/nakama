package server

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"fmt"

	"github.com/gofrs/uuid/v5"
	"github.com/heroiclabs/nakama-common/api"
	"github.com/heroiclabs/nakama-common/rtapi"
	"github.com/heroiclabs/nakama-common/runtime"
	"github.com/heroiclabs/nakama/v3/server/evr"
	"github.com/samber/lo"
	"go.uber.org/zap"
)

// sendDiscordError sends an error message to the user on discord
func sendDiscordError(e error, discordId string, logger *zap.Logger, discordRegistry DiscordRegistry) {
	// Message the user on discord
	bot := discordRegistry.GetBot()
	if bot != nil && discordId != "" {
		channel, err := bot.UserChannelCreate(discordId)
		if err != nil {
			logger.Warn("Failed to create user channel", zap.Error(err))
		}
		_, err = bot.ChannelMessageSend(channel.ID, fmt.Sprintf("Failed to register game server: %v", e))
		if err != nil {
			logger.Warn("Failed to send message to user", zap.Error(err))
		}
	}
}

// errFailedRegistration sends a failure message to the broadcaster and closes the session
func errFailedRegistration(session *sessionWS, logger *zap.Logger, err error, code evr.BroadcasterRegistrationFailureCode) error {
	logger.Warn("Failed to register game server", zap.Error(err))
	if err := session.SendEvr(
		evr.NewBroadcasterRegistrationFailure(code),
	); err != nil {
		return fmt.Errorf("failed to send lobby registration failure: %v", err)
	}

	session.Close(err.Error(), runtime.PresenceReasonDisconnect)
	return fmt.Errorf("failed to register game server: %v", err)
}

// broadcasterSessionEnded is called when the broadcaster has ended the session.
func (p *EvrPipeline) broadcasterSessionEnded(ctx context.Context, logger *zap.Logger, session *sessionWS, in evr.Message) error {
	// The broadcaster has ended the session.
	// shutdown the match. A new parking match will be created in response to the match leave message.
	matchID, ok := p.matchBySessionID.Load(session.ID().String())
	if ok {
		logger = logger.With(zap.String("mid", matchID))
	}

	go func() {
		select {
		case <-session.Context().Done():
			return
		case <-time.After(3 * time.Second):
		}

		// Leave the old match
		matchID, found := p.matchBySessionID.Load(session.ID().String())
		if !found {
			logger.Warn("Broadcaster session ended, but no match found")
			return
		}
		// Leave the old match
		leavemsg := &rtapi.Envelope{
			Message: &rtapi.Envelope_MatchLeave{
				MatchLeave: &rtapi.MatchLeave{
					MatchId: matchID,
				},
			},
		}

		if ok := session.pipeline.ProcessRequest(logger, session, leavemsg); !ok {
			logger.Error("Failed to process leave request")
		}
		//
		config, found := p.broadcasterRegistrationBySession.Load(session.ID().String())
		if !found {
			logger.Error("broadcaster session not found")
		}

		<-time.After(5 * time.Second)

		err := p.newParkingMatch(logger, session, config)
		if err != nil {
			logger.Error("Failed to create new parking match", zap.Error(err))
		}
	}()
	return nil
}

// broadcasterRegistrationRequest is called when the broadcaster has sent a registration request.
func (p *EvrPipeline) broadcasterRegistrationRequest(ctx context.Context, logger *zap.Logger, session *sessionWS, in evr.Message) error {
	request := in.(*evr.BroadcasterRegistrationRequest)
	discordId := ""

	// server connections are authenticated by discord ID and password.
	// Get the discordId and password from the context
	// Get the tags and guilds from the url params
	discordId, password, tags, guildIds, regions, err := extractAuthenticationDetailsFromContext(ctx)
	if err != nil {
		return errFailedRegistration(session, logger, err, evr.BroadcasterRegistration_Unknown)
	}

	logger = logger.With(zap.String("discordId", discordId), zap.Strings("guildIds", guildIds), zap.Strings("tags", tags), zap.Strings("regions", lo.Map(regions, func(v evr.Symbol, _ int) string { return v.String() })))

	// Assume that the regions provided are the ONLY regions the broadcaster wants to host in
	if len(regions) == 0 {
		regions = append(regions, evr.DefaultRegion)
	}

	if request.Region != evr.DefaultRegion {
		regions = append(regions, request.Region)
	}

	// Authenticate the broadcaster
	userId, _, err := p.authenticateBroadcaster(ctx, logger, session, discordId, password, guildIds, tags)
	if err != nil {
		return errFailedRegistration(session, logger, err, evr.BroadcasterRegistration_AccountDoesNotExist)
	}

	logger = logger.With(zap.String("userId", userId))

	// Set the external address in the request (to use for the registration cache).
	externalIP := net.ParseIP(session.ClientIP())
	if isPrivateIP(externalIP) {
		logger.Warn("Broadcaster is on a private IP, using this systems external IP", zap.String("privateIP", externalIP.String()), zap.String("externalIP", p.externalIP.String()))
		externalIP = p.externalIP
	}

	if len(tags) > 0 {
		if lo.Contains(tags, "membersonly") {
			// Servers that are members only are not available for public hosting
			tags = append(tags, "regionrequired")
		}
	}

	// Create the broadcaster config

	// Get the hosted groupIDs
	groupIDs, err := p.getBroadcasterHostGroups(ctx, logger, session, userId, discordId, guildIds)
	if err != nil {
		return errFailedRegistration(session, logger, err, evr.BroadcasterRegistration_Unknown)
	}

	presence := BroadcasterPresence{
		ServerID: evr.Symbol(request.ServerId),
		Endpoint: evr.Endpoint{
			InternalIP: request.InternalIP,
			ExternalIP: externalIP,
			Port:       request.Port,
		},
		VersionLock:  evr.Symbol(request.VersionLock),
		Region:       evr.Symbol(request.Region),
		Channels:     channels,
		Tags:         tags,
		UserID:       uuid.FromStringOrNil(userId),
		SessionID:    uuid.FromStringOrNil(session.id.String()),
		Username:     session.Username(),
		ClientIP:     session.ClientIP(),
		EvrID:        evr.EvrId{},
		DiscordID:    discordId,
		SessionFlags: ctx.Value(ctxFlagsKey{}).(SessionFlags),
		Node:         p.node,
		session:      session,
	}

	logger = logger.With(zap.String("internalIP", request.InternalIP.String()), zap.String("externalIP", externalIP.String()), zap.Uint16("port", request.Port))
	// Validate connectivity to the broadcaster.
	// Wait 2 seconds, then check

	time.Sleep(2 * time.Second)

	alive := false

	// Check if the broadcaster is available
	retries := 5
	var rtt time.Duration
	for i := 0; i < retries; i++ {
		rtt, err = BroadcasterHealthcheck(p.localIP, presence.Endpoint.ExternalIP, int(presence.Endpoint.Port), 500*time.Millisecond)
		if err != nil {
			logger.Warn("Failed to healthcheck broadcaster", zap.Error(err))
			time.Sleep(500 * time.Millisecond)
			continue
		}
		if rtt >= 0 {
			alive = true
			break
		}
	}
	if !alive {
		// If the broadcaster is not available, send an error message to the user on discord
		errorMessage := fmt.Sprintf("Broadcaster (Endpoint ID: %s, Server ID: %d) could not be reached. Error: %v", presence.Endpoint.ID(), presence.ServerID, err)
		go sendDiscordError(errors.New(errorMessage), discordId, logger, p.discordRegistry)
		return errFailedRegistration(session, logger, errors.New(errorMessage), evr.BroadcasterRegistration_Failure)
	}

	p.broadcasterRegistry.Add(presence)

	// Send the registration success message
	if err := session.SendEvr(
		evr.NewBroadcasterRegistrationSuccess(uint64(presence.ServerID), presence.Endpoint.ExternalIP),
		evr.NewSTcpConnectionUnrequireEvent(),
	); err != nil {
		return errFailedRegistration(session, logger, fmt.Errorf("failed to send lobby registration failure: %v", err), evr.BroadcasterRegistration_Failure)
	}

	return nil
}

func extractAuthenticationDetailsFromContext(ctx context.Context) (discordId, password string, tags []string, guildIds []string, err error) {
	discordAuthID, userPassword, ok := parseBasicAuth(ctx.Value(ctxBasicAuthKey{}).(string))
	if !ok {
		err = ErrNoAuthProvided
		return
	}
	tags = ctx.Value(ctxTagsKey{}).([]string)
	guildIds = ctx.Value(ctxDiscordGuildIDsKey{}).([]string)
	return discordAuthID, userPassword, tags, guildIds, nil
	regions = make([]evr.Symbol, 0)

	if regionstr, ok := params["regions"]; ok {
		for _, regionstr := range regionstr {
			for _, region := range strings.Split(regionstr, ",") {
				s := strings.Trim(region, " ")
				if s != "" {
					regions = append(regions, evr.ToSymbol(s))
				}
			}
		}
	}
	// Get the guilds that the broadcaster wants to host for
	guildIds = make([]string, 0)
	if guildparams, ok := params["guilds"]; ok {
		for _, guildstr := range guildparams {
			for _, guildId := range strings.Split(guildstr, ",") {
				s := strings.Trim(guildId, " ")
				if s != "" {
					guildIds = append(guildIds, s)
				}
			}
		}
	}
	return discordId, password, tags, guildIds, regions, nil
}

func (p *EvrPipeline) authenticateBroadcaster(ctx context.Context, logger *zap.Logger, session *sessionWS, discordId, password string, guildIds []string, tags []string) (string, string, error) {
	// Get the user id from the discord id
	uid, err := p.discordRegistry.GetUserIdByDiscordId(ctx, discordId, false)
	if err != nil {
		return "", "", fmt.Errorf("failed to find user for Discord ID: %v", err)
	}
	userId := uid.String()
	// Authenticate the user
	userId, username, _, err := AuthenticateEmail(ctx, logger, session.pipeline.db, userId+"@"+p.placeholderEmail, password, "", false)
	if err != nil {
		return "", "", fmt.Errorf("password authentication failure")
	}
	p.logger.Info("Authenticated broadcaster", zap.String("operator_userID", userId), zap.String("operator_username", username))

	// The broadcaster is authenticated, set the userID as the broadcasterID and create a broadcaster session
	// Broadcasters are not linked to the login session, they have a generic username and only use the serverdb path.
	err = session.BroadcasterSession(userId, "broadcaster:"+username)
	if err != nil {
		return "", "", fmt.Errorf("failed to create broadcaster session: %v", err)
	}

	return userId, username, nil
}

func (p *EvrPipeline) getUserGroups(ctx context.Context, userID uuid.UUID, minState api.GroupUserList_GroupUser_State) ([]*api.UserGroupList_UserGroup, error) {
	cursor := ""
	groups := make([]*api.UserGroupList_UserGroup, 0)
	for {
		usergroups, cursor, err := p.runtimeModule.UserGroupsList(ctx, userID.String(), 100, nil, cursor)
		if err != nil {
			return nil, fmt.Errorf("failed to get user groups: %v", err)
		}
		for _, g := range usergroups {
			if api.GroupUserList_GroupUser_State(g.State.GetValue()) <= minState {
				groups = append(groups, g)
			}
		}
		if cursor == "" {
			break
		}
	}
	return groups, nil
}

func (p *EvrPipeline) getBroadcasterHostGroups(ctx context.Context, logger *zap.Logger, session *sessionWS, userId, discordID string, guildIDs []string) (groupIDs []uuid.UUID, err error) {

	// Get the user's guild memberships
	memberships, err := p.discordRegistry.GetGuildGroupMemberships(ctx, uuid.FromStringOrNil(userId), nil)
	if err != nil {
		return nil, fmt.Errorf("failed to get user's guild groups: %v", err)
	}

		// Create a slice of user's guild group IDs
		for _, g := range groups {
			_, md, err := p.discordRegistry.GetGuildGroupMetadata(ctx, g.GetId())
			if err != nil {
				logger.Warn("Failed to get guild group metadata", zap.String("groupId", g.GetId()), zap.Error(err))
				continue
			}

			guildIDs = append(guildIDs, md.GuildID)
		}
	}

	desired := make([]GuildGroupMembership, 0, len(guildIDs))
	for _, guildID := range guildIDs {
		if guildID == "" {
			continue
		}

		// Get the guild member
		member, err := p.discordRegistry.GetGuildMember(ctx, guildID, discordID)
		if err != nil {
			logger.Warn("User not a member of the guild", zap.String("guildId", guildID))
			continue
		}

		// Get the group id for the guild
		groupID, found := p.discordRegistry.Get(guildID)
		if !found {
			logger.Warn("Guild not found", zap.String("guildId", guildID))
			continue
		}

		// Get the guild's metadata
		_, md, err := p.discordRegistry.GetGuildGroupMetadata(ctx, groupID)
		if err != nil {
			logger.Warn("Failed to get guild group metadata", zap.String("groupId", groupID), zap.Error(err))
			continue
		}

		// If the broadcaster role is blank, add it to the channels
		if md.BroadcasterHostRole == "" {
			allowed = append(allowed, guildID)
			continue
		}

		// Verify the user has the broadcaster role
		if !lo.Contains(member.Roles, md.BroadcasterHostRole) {
			logger.Warn("User does not have the broadcaster role", zap.String("discordID", member.User.ID), zap.String("guildId", guildID))
			//continue
		}

		// Add the channel to the list of hosting channels
		allowed = append(allowed, guildID)
	}

	// Get the groupId for each guildId
	groupIds := make([]uuid.UUID, 0)
	for _, guildId := range allowed {
		groupId, found := p.discordRegistry.Get(guildId)
		if !found {
			logger.Warn("Guild not found", zap.String("guildId", guildId))
			continue
		}
		groupIds = append(groupIds, uuid.FromStringOrNil(groupId))
	}

	return groupIDs, nil
}

func BroadcasterPortScan(lIP net.IP, rIP net.IP, startPort, endPort int, timeout time.Duration) (map[int]time.Duration, []error) {

	// Prepare slices to store results
	rtts := make(map[int]time.Duration, endPort-startPort+1)
	errs := make([]error, endPort-startPort+1)

	var wg sync.WaitGroup
	var mu sync.Mutex // Mutex to avoid race condition
	for port := startPort; port <= endPort; port++ {
		wg.Add(1)
		go func(port int) {
			defer wg.Done()

			rtt, err := BroadcasterHealthcheck(lIP, rIP, port, timeout)
			mu.Lock()
			if err != nil {
				errs[port-startPort] = err
			} else {
				rtts[port] = rtt
			}
			mu.Unlock()
		}(port)
	}
	wg.Wait()

	return rtts, errs
}

func BroadcasterRTTcheck(lIP net.IP, rIP net.IP, port, count int, interval, timeout time.Duration) (rtts []time.Duration, err error) {
	// Create a slice to store round trip times (rtts)
	rtts = make([]time.Duration, count)
	// Set a timeout duration

	// Create a WaitGroup to manage concurrency
	var wg sync.WaitGroup
	// Add a task to the WaitGroup
	wg.Add(1)

	// Start a goroutine
	go func() {
		// Ensure the task is marked done on return
		defer wg.Done()
		// Loop 5 times
		for i := 0; i < count; i++ {
			// Perform a health check on the broadcaster
			rtt, err := BroadcasterHealthcheck(lIP, rIP, port, timeout)
			if err != nil {
				// If there's an error, set the rtt to -1
				rtts[i] = -1
				continue
			}
			// Record the rtt
			rtts[i] = rtt
			// Sleep for the duration of the ping interval before the next iteration
			time.Sleep(interval)
		}
	}()

	wg.Wait()
	return rtts, nil
}

func DetermineLocalIPAddress() (net.IP, error) {
	conn, err := net.Dial("udp", "8.8.8.8:80")
	if err != nil {
		return nil, err
	}
	return conn.LocalAddr().(*net.UDPAddr).IP, nil
}

func DetermineExternalIPAddress() (net.IP, error) {
	response, err := http.Get("https://api.ipify.org?format=text")
	if err != nil {
		return nil, err
	}

	defer response.Body.Close()

	data, _ := io.ReadAll(response.Body)
	addr := net.ParseIP(string(data))
	if addr == nil {
		return nil, errors.New("failed to parse IP address")
	}
	return addr, nil
}

func isPrivateIP(ip net.IP) bool {
	privateRanges := []string{
		"127.0.0.0/8",
		"10.0.0.0/8",
		"172.16.0.0/12",
		"192.168.0.0/16",
	}
	for _, cidr := range privateRanges {
		_, subnet, _ := net.ParseCIDR(cidr)
		if subnet.Contains(ip) {
			return true
		}
	}
	return false
}
