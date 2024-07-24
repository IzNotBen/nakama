package server

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/bwmarrin/discordgo"
	"github.com/heroiclabs/nakama-common/rtapi"
	"github.com/heroiclabs/nakama/v3/server/evr"
	"github.com/samber/lo"
	"go.uber.org/zap"
)

func jsonWrap[T []byte | string](data T) string { return "```json\n" + string(data) + "\n```" }

func (p *EVRPipeline) DiscordRTAPIRelay(logger *zap.Logger, session *sessionWS, dg *discordgo.Session, in *rtapi.Envelope) ([]evr.Message, error) {
	// DM the user on discord

	messages := make([]string, 0)

	switch in.Message.(type) {
	case *rtapi.Envelope_StatusPresenceEvent, *rtapi.Envelope_MatchPresenceEvent, *rtapi.Envelope_StreamPresenceEvent:
	case *rtapi.Envelope_Party:
		leader := in.GetParty().GetLeader()
		members := lo.Map(in.GetParty().GetPresences(), func(p *rtapi.UserPresence, _ int) string { return "@" + p.GetUsername() })

		// Put the leader first
		for i, member := range members {
			if member == "@"+leader.GetUsername() {
				members[0], members[i] = members[i], members[0]
				break
			}
		}

		messages = append(messages, fmt.Sprintf("Active party: %s", strings.Join(members, ", ")))
	case *rtapi.Envelope_PartyLeader:

		messages = append(messages, "New party leader: @"+in.GetPartyLeader().GetPresence().GetUsername())

	case *rtapi.Envelope_PartyJoinRequest:

	case *rtapi.Envelope_PartyPresenceEvent:
		event := in.GetPartyPresenceEvent()

		joins := make([]string, 0)
		for _, join := range event.GetJoins() {
			joins = append(joins, fmt.Sprintf("@%s", join.GetUsername()))
		}
		leaves := make([]string, 0)
		for _, leave := range event.GetLeaves() {
			leaves = append(joins, fmt.Sprintf("@%s", leave.GetUsername()))
		}

		if len(joins) > 0 {
			messages = append(messages, fmt.Sprintf("Party join: %s\n", strings.Join(joins, ", ")))
		}
		if len(leaves) > 0 {
			messages = append(messages, fmt.Sprintf("Party leave: %s\n", strings.Join(leaves, ", ")))
		}

	case *rtapi.Envelope_MatchmakerMatched:
		msg := in.GetMatchmakerMatched()
		message := ""
		for _, user := range msg.GetUsers() {
			chunk, err := json.MarshalIndent(user, "", "  ")
			if err != nil {
				logger.Error("Failed to marshal user", zap.Error(err))
				continue
			}
			if len(message)+len(chunk) > 1900 {
				messages = append(messages, jsonWrap(message))
				message = ""
			}
			message += string(chunk) + "\n"
		}
		if message != "" {
			messages = append(messages, jsonWrap(message))
		}

	default:
		data, err := json.MarshalIndent(in.GetMessage(), "", "  ")
		if err != nil {
			logger.Error("Failed to marshal message", zap.Error(err))
		}
		if len(data) > 1900 {
			// Break at closet new line to 1900 characters
			for i := 1900; i > 0; i-- {
				if data[i] == '\n' {
					messages = append(messages, jsonWrap(data[:i]))
				}
			}
			messages = append(messages, jsonWrap(data))
		}
	}

	if len(messages) > 0 {
		if dg := p.discordRegistry.GetBot(); dg == nil {
			// No discord bot
		} else if discordID, err := GetDiscordIDByUserID(session.Context(), session.pipeline.db, session.UserID().String()); err != nil {
			logger.Warn("Failed to get discord ID", zap.Error(err))
		} else if channel, err := dg.UserChannelCreate(discordID); err != nil {
			logger.Warn("Failed to create DM channel", zap.Error(err))
		} else {
			for _, content := range messages {
				if _, err := dg.ChannelMessageSend(channel.ID, content); err != nil {
					logger.Warn("Failed to send message to user", zap.Error(err))
				}
			}
		}
	}

	return nil, nil
}
