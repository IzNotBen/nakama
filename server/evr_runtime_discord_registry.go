package server

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/bwmarrin/discordgo"
	"github.com/go-playground/validator/v10"
	"github.com/gofrs/uuid/v5"
	"github.com/heroiclabs/nakama-common/api"
	"github.com/heroiclabs/nakama-common/runtime"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const (
	DiscordRegistryLookupCollection = "DiscordRegistry"
	DiscordRegistryLookupKey        = "LookupTables"
)

var (
	validate = validator.New(validator.WithRequiredStructEnabled())

	ErrGroupIsNotaGuild = fmt.Errorf("group is not a guild")
)

type LookupTable struct {
	sync.RWMutex
	Store map[string]string `json:"store"`
}

type DiscordRegistry interface {
	Get(discordId string) (nakamaId string, ok bool)
	GetBot() *discordgo.Session
	Logger() runtime.Logger
	RuntimeModule() runtime.NakamaModule
	Store(discordId string, nakamaId string)
	ClearCache(discordId string)
	GetDiscordIdByUserId(ctx context.Context, userId uuid.UUID) (discordId string, err error)
	GetUserIdByUsername(ctx context.Context, username string, create bool) (userId uuid.UUID, err error)
	SyncronizeDiscordToNakamaAccount(ctx context.Context, userId uuid.UUID) error
	GetUserIdByDiscordId(ctx context.Context, discordId string, create bool) (userId uuid.UUID, err error)
	GetGuildByGroupId(ctx context.Context, groupId uuid.UUID) (guild *discordgo.Guild, found bool, err error)
	ReplaceMentions(guildID, s string) string
	PopulateCache() (cnt int, err error)
	GetGuildGroupMetadata(ctx context.Context, groupId string) (group *api.Group, metadata GroupMetadata, err error)
	SetGuildGroupMetadata(ctx context.Context, groupId string, metadata *GroupMetadata) error
	// GetGuildMember looks up the Discord member by the guild ID and member ID. Potentially using the state cache.
	GetGuildMember(ctx context.Context, guildId, memberId string) (*discordgo.Member, error)
	SynchronizeGroup(ctx context.Context, guild *discordgo.Guild) error
	GetGuild(ctx context.Context, guildId string) (guild *discordgo.Guild, found bool, err error)
	// GetGuildGroupMemberships looks up the guild groups by the user ID
	GetGuildGroupMemberships(ctx context.Context, userId uuid.UUID, groupIDs []uuid.UUID) ([]GuildGroupMembership, error)
	// GetUser looks up the Discord user by the user ID. Potentially using the state cache.
	GetUser(ctx context.Context, discordId string) (*discordgo.User, error)
	UpdateGuildGroupsForUser(ctx context.Context, userID uuid.UUID, guilds []*discordgo.Guild) ([]ChannelMember, error)
	isModerator(ctx context.Context, guildID, discordID string) (isModerator bool, isGlobal bool, err error)
	IsGlobalModerator(ctx context.Context, userID uuid.UUID) (ok bool, err error)
	SendMessage(ctx context.Context, userID uuid.UUID, message string) error
}

type TTLCache struct {
	store sync.Map
}

func NewTTLCache() *TTLCache {
	return &TTLCache{}
}

func (c *TTLCache) Set(key string, value interface{}, ttl time.Duration) {
	c.store.Store(key, value)
	time.AfterFunc(ttl, func() {
		c.store.Delete(key)
	})
}

func (c *TTLCache) Get(key string) (interface{}, bool) {
	return c.store.Load(key)
}

func (c *TTLCache) Delete(key string) {
	c.store.Delete(key)
}

func (c *TTLCache) GetGroupMetadata(groupId string) (GroupMetadata, bool) {
	if v, ok := c.Get(groupId); ok {
		return v.(GroupMetadata), ok
	}
	return GroupMetadata{}, false
}

// The discord registry is a storage-backed lookup table for discord user ids to nakama user ids.
// It also carries the bot session and a cache for the lookup table.
type LocalDiscordRegistry struct {
	sync.RWMutex
	ctx      context.Context
	nk       runtime.NakamaModule
	logger   runtime.Logger
	metrics  Metrics
	pipeline *Pipeline

	bot       *discordgo.Session // The bot
	botUserID uuid.UUID

	logChannel    *discordgo.Channel
	cache         sync.Map // Generic cache for map[discordId]nakamaId lookup
	metadataCache *TTLCache
}

func NewLocalDiscordRegistry(ctx context.Context, nk runtime.NakamaModule, logger runtime.Logger, metrics Metrics, config Config, pipeline *Pipeline, dg *discordgo.Session, logChannelID string) (r *LocalDiscordRegistry) {
	var err error

	dg.StateEnabled = true

	discordRegistry := &LocalDiscordRegistry{
		ctx:           ctx,
		nk:            nk,
		logger:        logger,
		metrics:       metrics,
		pipeline:      pipeline,
		bot:           dg,
		cache:         sync.Map{},
		metadataCache: NewTTLCache(),
	}

	dg.AddHandler(func(s *discordgo.Session, m *discordgo.Ready) {
		discordRegistry.PopulateCache()

		if logChannelID != "" {
			discordRegistry.logChannel, err = dg.Channel(logChannelID)
			if err != nil {
				logger.Error("Error getting log channel: %v", err)
			}
		}
	})

	return discordRegistry
}

func (r *LocalDiscordRegistry) GetBot() *discordgo.Session {
	return r.bot
}

func (r *LocalDiscordRegistry) Logger() runtime.Logger {
	return r.logger
}

func (r *LocalDiscordRegistry) RuntimeModule() runtime.NakamaModule {
	return r.nk
}

// PopulateCache populates the lookup cache with all the guilds and their roles
func (r *LocalDiscordRegistry) PopulateCache() (cnt int, err error) {
	ctx, cancel := context.WithTimeout(r.ctx, 10*time.Second)
	defer cancel()

	userID, err := r.GetUserIdByDiscordId(ctx, r.bot.State.User.ID, true)
	if err == nil {
		r.botUserID = userID
	}
	// Populate the cache with all the guild groups
	cnt = 0
	var groups []*api.Group
	cursor := ""
	for {
		groups, cursor, err = r.nk.GroupsList(ctx, "", "guild", nil, nil, 100, cursor)
		if err != nil {
			return
		}
		for _, group := range groups {
			// Check if the cache already has this group -> discordId entry
			if d, ok := r.Get(group.Id); ok {
				// Check that the reverse is true
				if g, ok := r.Get(d); ok {
					if g == group.Id {
						continue
					}
				}
			}

			metadata := &GroupMetadata{}
			if err := json.Unmarshal([]byte(group.Metadata), metadata); err != nil {
				r.logger.Warn(fmt.Sprintf("Error unmarshalling group metadata for group %s:  %s", group.Id, err))
			}

			if metadata.GuildID != "" {
				r.Store(metadata.GuildID, group.Id)
				r.Store(group.Id, metadata.GuildID)
				guild, found, err := r.GetGuild(r.ctx, metadata.GuildID)
				if err != nil {
					r.logger.Warn("Error getting guild %s: %s", metadata.GuildID, err)
				}
				if !found {
					r.logger.Warn(fmt.Sprintf("Removing group %s", group.Id))
					r.removeGuildGroups(r.ctx, metadata.GuildID)
					continue
				}

				r.bot.State.GuildAdd(guild)
				cnt++
			}

			mapping := map[string]string{
				metadata.ModeratorRole:       metadata.ModeratorGroupID,
				metadata.BroadcasterHostRole: metadata.BroadcasterHostGroupID,
			}

			for roleId, groupId := range mapping {
				if roleId != "" && groupId != "" {
					// Verify the cache entry
					entry, found := r.Get(roleId)
					if found && entry != groupId {
						r.logger.Warn(fmt.Sprintf("Role %s does not match group %s", roleId, groupId))
					}
					// Verify the reverse
					entry, found = r.Get(groupId)
					if found && entry != roleId {
						r.logger.Warn(fmt.Sprintf("Group %s does not match role %s", groupId, roleId))
						continue
					}

					// Verify that the role exists on the guild
					_, err := r.bot.State.Role(metadata.GuildID, roleId)
					if err != nil {
						r.logger.Warn(fmt.Sprintf("Error getting role %s for guild %s: %s", roleId, metadata.GuildID, err))
						continue
					}

					// Verify the group exists and has the correct guildId
					groups, err := r.nk.GroupsGetId(r.ctx, []string{groupId})
					if err != nil {
						r.logger.Warn(fmt.Sprintf("Error getting role group %s: %s", groupId, err))
						continue
					}
					if len(groups) == 0 {
						r.logger.Warn(fmt.Sprintf("Role group %s does not exist", groupId))
						continue
					}
					group := groups[0]
					md := &GroupMetadata{}
					if err := json.Unmarshal([]byte(group.GetMetadata()), md); err != nil {
						r.logger.Warn(fmt.Sprintf("Error unmarshalling group metadata for group %s:  %s", group.Id, err))
						continue
					}
					if md.GuildID != metadata.GuildID {
						r.logger.Warn(fmt.Sprintf("Role group %s does not belong to guild %s", groupId, metadata.GuildID))
						continue
					}
					r.Store(roleId, groupId)
					r.Store(groupId, roleId)
					cnt++
				}
			}
		}
		if cursor == "" {
			break
		}
	}

	r.logger.Info("Populated registry lookup cache with %d guilds/roles/users", cnt)
	return
}

// Get looks up the Nakama group ID by the Discord guild or role ID from cache.
func (r *LocalDiscordRegistry) Get(discordId string) (nakamaId string, ok bool) {
	if v, ok := r.cache.Load(discordId); ok {
		return v.(string), ok
	}
	return "", false
}

// Store adds or updates the Nakama group ID by the Discord guild or role ID
func (r *LocalDiscordRegistry) Store(discordID string, nakamaID string) {
	if discordID == "" || nakamaID == "" {
		r.logger.Error("discordId and nakamaId cannot be nil")
	}
	if discordID == "0" || nakamaID == "00000000-0000-0000-0000-000000000000" {
		r.logger.Error("discordId and nakamaId cannot be nil")
	}
	r.cache.Store(discordID, nakamaID)
}

// Delete removes the Nakama group ID by the Discord guild or role ID
func (r *LocalDiscordRegistry) ClearCache(discordId string) {
	r.cache.Delete(discordId)
}

// GetUser looks up the Discord user by the user ID. Potentially using the state cache.
func (r *LocalDiscordRegistry) GetUser(ctx context.Context, discordId string) (*discordgo.User, error) {
	if discordId == "" {
		return nil, fmt.Errorf("discordId is required")
	}

	// Try to find the user in a guild state first.
	for _, guild := range r.bot.State.Guilds {
		if member, err := r.bot.State.Member(guild.ID, discordId); err == nil {
			if member.User == nil || member.User.Username == "" || member.User.GlobalName == "" {
				continue
			}
			return member.User, nil
		}
	}

	// Get it from the API
	return r.bot.User(discordId)
}

// GetGuild looks up the Discord guild by the guild ID. Potentially using the state cache.
func (r *LocalDiscordRegistry) GetGuild(ctx context.Context, guildId string) (guild *discordgo.Guild, found bool, err error) {

	if guildId == "" {
		return nil, false, fmt.Errorf("guildId is required")
	}
	// Check the cache
	if guild, err := r.bot.State.Guild(guildId); err == nil {
		return guild, false, nil
	}
	guild, err = r.bot.Guild(guildId)
	if err != nil {
		// Check hte HTTP error
		if e, ok := err.(*discordgo.RESTError); ok {
			if e.Response.StatusCode == 404 {
				return nil, false, nil
			}
		}
		return nil, false, fmt.Errorf("error getting guild %s: %w", guildId, err)
	}
	return guild, true, nil
}

// GetGuildByGroupId looks up the Discord guild by the group ID. Potentially using the state cache.
func (r *LocalDiscordRegistry) GetGuildByGroupId(ctx context.Context, groupID uuid.UUID) (guild *discordgo.Guild, found bool, err error) {
	if groupID == uuid.Nil {
		return nil, false, fmt.Errorf("guildId is required")
	}
	// Get the guild group metadata
	_, md, err := r.GetGuildGroupMetadata(ctx, groupID.String())
	if err != nil {
		return nil, false, fmt.Errorf("error getting guild group metadata: %w", err)
	}
	return r.GetGuild(ctx, md.GuildID)

}

// GetUserIdByMemberId looks up the Nakama user ID by the Discord user ID
func (r *LocalDiscordRegistry) GetUserIdByUsername(ctx context.Context, username string, create bool) (userId uuid.UUID, err error) {
	if username == "" {
		return userId, fmt.Errorf("username is required")
	}

	// Lookup the user by the username
	users, err := r.nk.UsersGetUsername(ctx, []string{username})
	if err != nil {
		return userId, err
	}
	if len(users) == 0 {
		return userId, status.Error(codes.NotFound, "User not found")
	}
	userId = uuid.FromStringOrNil(users[0].Id)
	return userId, nil
}

// GetGuildMember looks up the Discord member by the guild ID and member ID. Potentially using the state cache.
func (r *LocalDiscordRegistry) GetGuildMember(ctx context.Context, guildId, memberId string) (*discordgo.Member, error) {
	// Check if guildId and memberId are provided
	if guildId == "" {
		return nil, fmt.Errorf("guildId is required")
	}
	if memberId == "" {
		return nil, fmt.Errorf("memberId is required")
	}

	// Try to find the member in the guild state (cache) first
	if member, err := r.bot.State.Member(guildId, memberId); err == nil {
		return member, nil
	}

	// If member is not found in the cache, get it from the API
	member, err := r.bot.GuildMember(guildId, memberId)
	if err != nil {
		if restErr, ok := err.(*discordgo.RESTError); ok && restErr.Message.Code == discordgo.ErrCodeUnknownMember {
			return nil, nil
		}
		return nil, fmt.Errorf("error getting member %s in guild %s: %w", memberId, guildId, err)
	}
	r.bot.State.MemberAdd(member)

	return member, nil
}

func (r *LocalDiscordRegistry) GetGuildGroupMetadata(ctx context.Context, groupID string) (group *api.Group, md GroupMetadata, err error) {
	const metadataCacheTTL = 5 * time.Minute
	// Check if groupId is provided
	if groupID == "" {
		return nil, md, fmt.Errorf("groupId is required")
	}

	// Fetch the group using the provided groupId
	groups, err := r.nk.GroupsGetId(ctx, []string{groupID})
	if err != nil {
		return nil, md, fmt.Errorf("error getting group (%s): %w", groupID, err)
	}

	// Check if the group exists
	if len(groups) == 0 {
		return nil, md, fmt.Errorf("group not found: %s", groupID)
	}
	group = groups[0]

	if group.LangTag != "guild" {
		return nil, md, ErrGroupIsNotaGuild
	}

	var found bool
	if md, found = r.metadataCache.GetGroupMetadata(groupID); found {
		return group, md, nil
	}

	// Extract the metadata from the group
	data := group.GetMetadata()

	// Unmarshal the metadata into a GroupMetadata struct
	if err := json.Unmarshal([]byte(data), &md); err != nil {
		return nil, md, fmt.Errorf("error unmarshalling group metadata: %w", err)
	}

	// Update the lookup cache
	r.Store(groupID, md.GuildID)
	r.Store(md.GuildID, groupID)

	r.metadataCache.Set(groupID, md, metadataCacheTTL)

	// Return the unmarshalled GroupMetadata
	return group, md, nil
}

// GetGuildGroups looks up the guild groups by the user ID
func (r *LocalDiscordRegistry) GetGuildGroups(ctx context.Context, userId uuid.UUID) ([]*api.Group, error) {
	// Check if userId is provided
	if userID == uuid.Nil {
		return nil, fmt.Errorf("userId is required")
	}
	userIDStr := userID.String()
	memberships := make([]GuildGroupMembership, 0)
	cursor := ""
	for {
		// Fetch the groups using the provided userId
		userGroups, _, err := r.nk.UserGroupsList(ctx, userIDStr, 100, nil, cursor)
		if err != nil {
			return nil, fmt.Errorf("error getting user groups: %w", err)
		}
		for _, ug := range userGroups {
			g := ug.GetGroup()
			if g.GetLangTag() != "guild" {
				continue
			}
			if len(groupIDs) > 0 && !slices.Contains(groupIDs, uuid.FromStringOrNil(g.GetId())) {
				continue
			}

			membership := NewGuildGroupMembership(g, userID, api.UserGroupList_UserGroup_State(ug.GetState().GetValue()))

			memberships = append(memberships, membership)
		}
		if cursor == "" {
			break
		}
	}
	return memberships, nil
}

// SyncronizeDiscordToNakamaAccount updates the Nakama account with the Discord user data
func (r *LocalDiscordRegistry) SyncronizeDiscordToNakamaAccount(ctx context.Context, userID uuid.UUID) error {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()

	discordID, err := r.GetDiscordIdByUserId(ctx, userID)
	if err != nil {
		return fmt.Errorf("error getting discord id: %v", err)
	}

	timer := time.Now()
	if r.metrics != nil {
		defer func() {
			r.metrics.CustomTimer("SyncronizeDiscordToNakamaAccount_duration_timer", nil, time.Since(timer)*time.Millisecond)
		}()
	}

	// Get the discord User
	u, err := r.GetUser(ctx, discordID)
	if err != nil {
		return fmt.Errorf("error getting discord user: %v", err)
	}

	// Map Discord user data onto Nakama account data
	username := u.Username
	s := strings.SplitN(u.Locale, "-", 2)
	langTag := s[0]
	avatar := u.AvatarURL("512")

	// Update the basic account details

	if err := r.nk.AccountUpdateId(ctx, userID.String(), username, nil, "", "", "", langTag, avatar); err != nil {
		return fmt.Errorf("error updating account: %v", err)
	}

	defer r.Store(discordID, userID.String())
	defer r.Store(userID.String(), discordID)

	return nil
}

type ChannelMember struct {
	ChannelID              uuid.UUID // Group ID / Channel ID
	ChannelName            string
	DisplayName            string
	isSuspended            bool
	isModerator            bool
	isBroadcasterHost      bool
	ExcludeFromMatchmaking bool // Does the user want to matchmake with this channel

}

func (r *LocalDiscordRegistry) updateGuildGroup(ctx context.Context, userID uuid.UUID, userGroupIDs []string, guildID, discordID string) (echoMember ChannelMember, err error) {
	// If discord bot is not responding, return
	if r.bot == nil {
		return echoMember, fmt.Errorf("discord bot is not responding")
	}

	// Get the guild's group ID
	groupID, found := r.Get(guildID)
	if !found {
		return echoMember, fmt.Errorf("group not found for guild %s", guildID)
	}

	// Get the guild's group metadata
	group, md, err := r.GetGuildGroupMetadata(ctx, groupID)
	if err != nil {
		return echoMember, fmt.Errorf("error getting guild group metadata: %w", err)
	}

	// Get the guild member (if they exist)
	member, err := r.GetGuildMember(ctx, guildID, discordID)
	if err != nil {
		if ctx.Err() != nil {
			// This is if the context timed out.
			return echoMember, fmt.Errorf("context cancelled: %w", err)
		}
		return echoMember, fmt.Errorf("error getting guild member: %w", err)
	}
	if member == nil {
		return echoMember, fmt.Errorf("guild member %s not found in guild %s", discordID, guildID)
	}

	// Check if the user is suspended
	isSuspended := len(lo.Intersect(member.Roles, md.SuspensionRoles)) > 0

	// Disconnect user match sessions if suspended
	if isSuspended {
		err := r.disconnectUserMatchSessions(ctx, userID.String())
		if err != nil {
			return echoMember, fmt.Errorf("error disconnecting user match sessions: %w", err)
		}
	}

	// Map of role to group ID
	roleGroupMap := map[string]string{
		"":                     groupID,
		md.ModeratorRole:       md.ModeratorGroupID,
		md.BroadcasterHostRole: md.BroadcasterHostGroupID,
	}

	// Current guild role groups
	guildGroupIDs := lo.MapToSlice(roleGroupMap, func(r, g string) string { return g })
	currentGroupIDs := lo.Intersect(userGroupIDs, guildGroupIDs)

	// Calculate the correct group IDs
	correctGroupIDs := make([]string, 0, len(roleGroupMap))
	for r, g := range roleGroupMap {
		if r == "" || slices.Contains(member.Roles, r) {
			correctGroupIDs = append(correctGroupIDs, g)
		}
	}

	// Add or remove the user from the correct groups
	adds, removes := lo.Difference(correctGroupIDs, currentGroupIDs)

	for _, groupID := range removes {
		r.logger.Debug("Removing user %s from group %s", userID, groupID)
		err = r.nk.GroupUsersKick(ctx, SystemUserID, groupID, []string{userID.String()})
		if err != nil {
			return echoMember, fmt.Errorf("error kicking user from group %s: %w", groupID, err)

		}
	}

	for _, groupID := range adds {
		r.logger.Debug("Adding user %s to group %s", userID, groupID)
		err = r.nk.GroupUsersAdd(ctx, SystemUserID, groupID, []string{userID.String()})
		if err != nil {
			return echoMember, fmt.Errorf("error adding user to group %s: %w", groupID, err)
		}

	}

	echoMember.ChannelID = uuid.FromStringOrNil(groupID)
	echoMember.ChannelName = group.Name
	echoMember.isSuspended = isSuspended
	echoMember.isModerator = slices.Contains(member.Roles, md.ModeratorRole)
	echoMember.isBroadcasterHost = slices.Contains(member.Roles, md.BroadcasterHostRole)

	echoMember.DisplayName, err = SetDisplayNameByChannelBySession(ctx, r.nk, r.logger, r, userID.String(), groupID)
	if err != nil {
		r.logger.WithField("error", err).Error("Error setting display name")
	}

	return echoMember, nil
}

func (r *LocalDiscordRegistry) disconnectUserMatchSessions(ctx context.Context, userID string) error {

	// If the player has a match connection, disconnect it.
	users, err := r.nk.StreamUserList(StreamModeEvr, userID, StreamContextMatch.String(), "", true, true)
	if err != nil {
		return fmt.Errorf("error getting user stream list: %w", err)
	}

	// Disconnect any matchmaking sessions (this will put them back to the login screen)
	for _, user := range users {
		// Disconnect the user
		if user.GetUserId() == userID {
			go func(userID, sessionID string) {
				r.logger.Debug("Disconnecting suspended user %s match session: %s", userID, sessionID)
				// Add a wait time, otherwise the user will not see the suspension message
				<-time.After(15 * time.Second)
				if err := r.nk.SessionDisconnect(ctx, sessionID, runtime.PresenceReasonDisconnect); err != nil {
					r.logger.Error("Error disconnecting suspended user: %w", err)
				}
			}(user.GetUserId(), user.GetSessionId())
		}
	}
	return nil
}

func (r *LocalDiscordRegistry) UpdateGuildGroupsForUser(ctx context.Context, userID uuid.UUID, guilds []*discordgo.Guild) (memberships []ChannelMember, err error) {
	memberships = make([]ChannelMember, 0)

	// Get the user's discord ID
	discordID, err := r.GetDiscordIdByUserId(ctx, userID)
	if err != nil {
		return memberships, fmt.Errorf("failed to get discord ID: %w", err)
	}

	// Get all of the user's userGroups
	userGroups, _, err := r.nk.UserGroupsList(ctx, userID.String(), 100, nil, "")
	if err != nil {
		return memberships, fmt.Errorf("error getting user groups: %v", err)
	}

	// Create the group id slice
	userGroupIDs := lo.Map(userGroups, func(g *api.UserGroupList_UserGroup, _ int) string { return g.Group.Id })

	// Get the group sizes
	edgeCounts := make(map[uuid.UUID]int)
	for _, group := range userGroups {
		edgeCounts[uuid.FromStringOrNil(group.Group.Id)] = int(group.GetGroup().GetEdgeCount())
	}

	// Check every guild the bot is in for this user.
	for _, guild := range guilds {

		echoMember, err := r.updateGuildGroup(ctx, userID, userGroupIDs, guild.ID, discordID)
		if err != nil {
			continue
		}
		memberships = append(memberships, echoMember)
	}

	// Sort the memberships by the group edgecount (descending)
	slices.SortFunc(memberships, func(a, b ChannelMember) int {
		return edgeCounts[b.ChannelID] - edgeCounts[a.ChannelID]
	})

	return memberships, nil
}

// GetUserIdByDiscordId looks up, or creates, the Nakama user ID by the Discord user ID; potentially using the cache.
func (r *LocalDiscordRegistry) GetUserIdByDiscordId(ctx context.Context, discordID string, create bool) (userID uuid.UUID, err error) {

	if discordID == "" {
		return userID, fmt.Errorf("discordId is required")
	}

	// Check the cache
	if s, found := r.Get(discordID); found {
		userID, err = uuid.FromString(s)
		if err != nil {
			return userID, fmt.Errorf("error parsing user id: %w", err)
		}
		return userID, nil
	}

	username := ""
	if create {
		// Get the user from discord
		discordUser, err := r.GetUser(ctx, discordID)
		if err != nil {
			return uuid.Nil, fmt.Errorf("error getting discord user %s: %w", discordID, err)
		}
		username = discordUser.Username
	}
	userIDstr, _, _, err := r.nk.AuthenticateCustom(ctx, discordID, username, create)
	if err != nil {
		return uuid.Nil, fmt.Errorf("error authenticating user %s: %w", discordID, err)
	}

	userID = uuid.FromStringOrNil(userIDstr)

	if userID == uuid.Nil {
		return uuid.Nil, fmt.Errorf("uuid is nil")
	}

	defer r.Store(discordID, userID.String())
	defer r.Store(userID.String(), discordID)

	return userID, err

}

func (r *LocalDiscordRegistry) GetGroupIDbyGuildID(ctx context.Context, guildID string) (groupID uuid.UUID, err error) {
	if guildID == "" {
		return uuid.Nil, fmt.Errorf("guildID is required")
	}

	if v, ok := r.cache.Load(guildID); ok {
		return uuid.FromStringOrNil(v.(string)), nil
	}

	return groupID, nil
}

// GetDiscordIdByUserId looks up the Discord user ID by the Nakama user ID; potentially using the cache.
func (r *LocalDiscordRegistry) GetDiscordIdByUserId(ctx context.Context, userId uuid.UUID) (discordId string, err error) {
	if userId == uuid.Nil {
		return "", fmt.Errorf("userId is required")
	}

	// Check the cache
	if v, ok := r.cache.Load(userId.String()); ok {
		return v.(string), nil
	}

	// Lookup the discord user by the nakama user id
	account, err := r.nk.AccountGetId(ctx, userId.String())
	if err != nil {
		return "", err
	}

	discordId = account.GetCustomId()

	// Store the discordId and userId in the cache when the function returns
	defer r.Store(discordId, userId.String())

	return discordId, nil
}

// ReplaceMentions replaces the discord user mentions with the user's display name
func (r *LocalDiscordRegistry) ReplaceMentions(guildID, s string) string {
	s = strings.Replace(s, "\\u003c", " <", -1)
	s = strings.Replace(s, "\\u003e", "> ", -1)
	f := strings.Fields(s)
	for i, v := range f {
		if strings.HasPrefix(v, "<@") && strings.HasSuffix(v, ">") {
			f[i] = strings.Trim(v, "<@>")
			u, err := r.bot.GuildMember(guildID, f[i])
			if err != nil {
				continue
			}
			f[i] = "@" + u.DisplayName()
		}
	}
	return strings.Join(f, " ")
}

func parseDuration(s string) (time.Duration, error) {

	f := strings.Fields(s)
	if len(f) != 2 {
		return 0, fmt.Errorf("invalid duration: invalid number of fields: %s", s)
	}
	d, err := strconv.Atoi(f[0])
	if err != nil {
		return 0, fmt.Errorf("invalid duration: unable to parse: %s", s)
	}

	switch f[1][:1] {
	case "s":
		return time.Duration(d) * time.Second, nil
	case "m":
		return time.Duration(d) * time.Minute, nil
	case "h":
		return time.Duration(d) * time.Hour, nil
	case "d":
		return time.Duration(d) * 24 * time.Hour, nil
	case "w":
		return time.Duration(d) * 7 * 24 * time.Hour, nil
	}
	return 0, fmt.Errorf("invalid duration: invalid unit: %s", s)
}

// Helper function to get or create a group
func (r *LocalDiscordRegistry) findOrCreateGroup(ctx context.Context, groupId, name, description, ownerId, langtype string, guild *discordgo.Guild) (*api.Group, error) {
	nk := r.nk
	var group *api.Group

	// Try to retrieve the group by ID
	if groupId != "" {
		groups, err := nk.GroupsGetId(ctx, []string{groupId})
		if err != nil {
			return nil, fmt.Errorf("error getting group by id: %w", err)
		}
		if len(groups) != 0 {
			group = groups[0]
		}
	}

	// Next attempt to find the group by name.
	if group == nil {
		if groups, _, err := nk.GroupsList(ctx, name, "", nil, nil, 1, ""); err == nil && len(groups) == 1 {
			group = groups[0]
		}
	}
	// If the group was found, update the lookup table

	// If the group wasn't found, create it
	if group == nil {
		md := GroupMetadata{
			GuildID: guild.ID,
		}
		gm, err := md.MarshalToMap()
		if err != nil {
			return nil, fmt.Errorf("error marshalling group metadata: %w", err)
		}
		// Create the group
		group, err = nk.GroupCreate(ctx, r.botUserID.String(), name, ownerId, langtype, description, guild.IconURL("512"), false, gm, 100000)
		if err != nil {
			return nil, fmt.Errorf("error creating group: %w", err)
		}
	}

	if langtype == "guild" {
		// Set the group in the registry
		r.Store(guild.ID, group.GetId())
	}

	return group, nil
}

func (r *LocalDiscordRegistry) SynchronizeGroup(ctx context.Context, guild *discordgo.Guild) error {
	var err error

	// Get the owner's nakama user
	uid, err := r.GetUserIdByDiscordId(ctx, guild.OwnerID, true)
	if err != nil {
		return fmt.Errorf("error getting guild owner id: %w", err)
	}
	ownerId := uid.String()
	// Check the lookup table for the guild group
	groupId, found := r.Get(guild.ID)
	if !found {
		groupId = ""
	}

	// Find or create the guild group
	guildGroup, err := r.findOrCreateGroup(ctx, groupId, guild.Name, guild.Description, ownerId, "guild", guild)
	if err != nil {
		return fmt.Errorf("error finding/creating guild group: %w", err)
	}

	// Unmarshal the guild group's metadata for updating.
	guildMetadata := &GroupMetadata{}
	if err := json.Unmarshal([]byte(guildGroup.GetMetadata()), guildMetadata); err != nil {
		return fmt.Errorf("error unmarshalling group metadata: %w", err)
	}

	// Set the group Id in the metadata so it can be found during an error.
	guildMetadata.GuildID = guild.ID

	// Find or create the moderator role group
	moderatorGroup, err := r.findOrCreateGroup(ctx, guildMetadata.ModeratorGroupID, guild.Name+" Moderators", guild.Name+" Moderators", ownerId, "role", guild)
	if err != nil {
		return fmt.Errorf("error getting or creating moderator group: %w", err)
	}
	guildMetadata.ModeratorGroupID = moderatorGroup.Id

	// Find or create the server role group
	serverGroup, err := r.findOrCreateGroup(ctx, guildMetadata.BroadcasterHostGroupID, guild.Name+" Broadcaster Hosts", guild.Name+" Broadcaster Hosts", ownerId, "role", guild)
	if err != nil {
		return fmt.Errorf("error getting or creating server group: %w", err)
	}
	guildMetadata.BroadcasterHostGroupID = serverGroup.Id

	// Set a default rules, or get the rules from the channel topic
	guildMetadata.RulesText = "No #rules channel found. Please create the channel and set the topic to the rules."
	channels, err := r.bot.GuildChannels(guild.ID)
	if err != nil {
		return fmt.Errorf("error getting guild channels: %w", err)
	}
	for _, channel := range channels {
		if channel.Type == discordgo.ChannelTypeGuildText && channel.Name == "rules" {
			guildMetadata.RulesText = channel.Topic
			break
		}
	}

	// Rewrite the guild groups metadata
	md, err := guildMetadata.MarshalToMap()
	if err != nil {
		return fmt.Errorf("error marshalling group metadata: %w", err)
	}

	// Update the guild group
	if err := r.nk.GroupUpdate(ctx, guildGroup.GetId(), SystemUserID, guild.Name, ownerId, "guild", guild.Description, guild.IconURL("512"), false, md, 100000); err != nil {
		return fmt.Errorf("error updating guild group: %w", err)
	}

	return nil
}
func (r *LocalDiscordRegistry) removeGuildGroups(ctx context.Context, guildID string) error {
	// Get the guild group metadata
	groupID, found := r.Get(guildID)
	if !found {
		return fmt.Errorf("group not found for guild %s", guildID)
	}

	// Get the guild group metadata
	_, md, err := r.GetGuildGroupMetadata(ctx, groupID)
	if err != nil {
		return fmt.Errorf("error getting guild group metadata: %w", err)
	}

	// Get the guild group's roles
	roles := []string{md.ModeratorGroupID, md.BroadcasterHostGroupID}
	for _, roleID := range roles {
		// Get the role group
		roleGroups, err := r.nk.GroupsGetId(ctx, []string{roleID})
		if err != nil {
			return fmt.Errorf("error getting role group: %w", err)
		}
		// Check if the role group exists
		if len(roleGroups) == 0 {
			continue
		}
		// Get the role group's metadata
		roleMd := &GroupMetadata{}
		if err := json.Unmarshal([]byte(roleGroups[0].GetMetadata()), roleMd); err != nil {
			return fmt.Errorf("error unmarshalling role group metadata: %w", err)
		}
		// Check if the role group belongs to the guild group
		if roleMd.GuildID != groupID {
			continue
		}
		// Delete the role group
		if err := r.nk.GroupDelete(ctx, roleID); err != nil {
			return fmt.Errorf("error deleting role group: %w", err)
		}
	}

	// Delete the guild group
	if err := r.nk.GroupDelete(ctx, groupID); err != nil {
		return fmt.Errorf("error deleting guild group: %w", err)
	}

	return nil
}

func (r *LocalDiscordRegistry) OnGuildMembersChunk(ctx context.Context, b *discordgo.Session, e *discordgo.GuildMembersChunk, logger runtime.Logger, nk runtime.NakamaModule, initializer runtime.Initializer) error {
	// Get the nakama group for the guild

	// Add all the members of the guild to the group, in chunks
	chunkSize := 10
	logger.Debug("Received guild member chunk %d of %d", chunkSize, len(e.Members))

	for i := 0; i < len(e.Members); i += chunkSize {
		members := e.Members[i:min(i+chunkSize, len(e.Members))]
		usernames := make([]string, len(members))
		for i, member := range members {
			usernames[i] = member.User.Username
		}

		users, err := nk.UsersGetUsername(ctx, usernames)
		if err != nil {
			return fmt.Errorf("error getting account Ids for guild members: %w", err)
		}

		accountIds := make([]string, len(users))
		for _, user := range users {
			accountIds[i] = user.Id
		}
		// Add the member to the group
		if err := nk.GroupUsersAdd(ctx, SystemUserID, members[0].GuildID, accountIds); err != nil {
			return fmt.Errorf("group add users error: %w", err)
		}
	}
	return nil
}

func (r *LocalDiscordRegistry) GetAllSuspensions(ctx context.Context, userId uuid.UUID) ([]*SuspensionStatus, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	// Get the discordId for the userId
	discordId, err := r.GetDiscordIdByUserId(ctx, userId)
	if err != nil {
		return nil, err
	}

	// Get a list of the bot's guilds
	groups, err := r.GetGuildGroupMemberships(ctx, userId, nil)
	if err != nil {
		return nil, err
	}

	// Get the metadata for each guild and it's suspension roles
	suspensions := make([]*SuspensionStatus, 0)
	for _, gm := range groups {
		group := gm.GuildGroup
		md := group.Metadata
		// Get the guild member's roles
		member, err := r.GetGuildMember(ctx, md.GuildID, discordId)
		if err != nil {
			return nil, fmt.Errorf("error getting guild member: %w", err)
		}

		// Look for an intersection between suspension roles and the member's roles
		if slices.Contains(member.Roles, md.SuspensionRole) {
			// Get the role's name
			role, err := r.bot.State.Role(md.GuildID, md.SuspensionRole)
			if err != nil {
				return nil, fmt.Errorf("error getting guild role: %w", err)
			}
			status := &SuspensionStatus{
				GuildID:       group.Id,
				GuildName:     group.Name,
				UserDiscordID: discordId,
				UserID:        userId.String(),
				RoleId:        roleId,
				RoleName:      role.Name,
			}
			// Apppend the suspension status to the list
			suspensions = append(suspensions, status)
		}
	}
	return suspensions, nil
}

func (r *LocalDiscordRegistry) isModerator(ctx context.Context, guildID, discordID string) (isModerator bool, isGlobal bool, err error) {

	userID, err := r.GetUserIdByDiscordId(ctx, discordID, false)
	if userID == uuid.Nil {
		return false, false, fmt.Errorf("error getting user id: %w", err)
	}

	groupID, found := r.Get(guildID)
	if !found {
		return false, false, fmt.Errorf("group not found for guild %s", guildID)
	}

	_, md, err := r.GetGuildGroupMetadata(ctx, groupID)
	if err != nil {
		return false, false, fmt.Errorf("error getting guild group metadata: %w", err)
	}

	// Get the member
	member, err := r.GetGuildMember(ctx, guildID, discordID)
	if err != nil {
		return false, false, fmt.Errorf("error getting guild member: %w", err)
	}

	// Check if the member has the moderator role
	return slices.Contains(member.Roles, md.ModeratorRole), false, nil
}

func (r *LocalDiscordRegistry) IsGlobalModerator(ctx context.Context, userID uuid.UUID) (bool, error) {
	return r.isSystemGroupMember(ctx, userID, "Global Moderators")
}

func (r *LocalDiscordRegistry) isSystemGroupMember(ctx context.Context, userID uuid.UUID, groupName string) (bool, error) {
	// Check if they are a member of the Global Moderators group
	groups, _, err := r.nk.UserGroupsList(ctx, userID.String(), 100, nil, "")
	if err != nil {
		return false, fmt.Errorf("error getting user groups: %w", err)
	}

	for _, g := range groups {
		if g.Group.LangTag == "system" && g.Group.Name == groupName {
			return true, nil
		}
	}
	return false, nil
}

func (r *LocalDiscordRegistry) SendMessage(ctx context.Context, userID uuid.UUID, message string) error {

	discordID, found := r.GetDiscordIdByUserId(ctx, userID)
	if found != nil {
		return fmt.Errorf("error getting discord id: %w", found)
	}

	channel, err := r.bot.UserChannelCreate(discordID)
	if err != nil {
		return fmt.Errorf("error creating user channel: %w", err)
	}

	_, err = r.bot.ChannelMessageSend(channel.ID, message)
	if err != nil {
		return fmt.Errorf("error sending message: %w", err)
	}
	return nil
}
