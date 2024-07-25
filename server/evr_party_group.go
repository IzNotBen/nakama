package server

import (
	"github.com/gofrs/uuid/v5"
	"github.com/heroiclabs/nakama-common/rtapi"
	"github.com/heroiclabs/nakama-common/runtime"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type PartyGroup struct {
	name string
	ph   *PartyHandler
	self *rtapi.UserPresence
}

func (pg *PartyGroup) ID() uuid.UUID {
	pg.ph.RLock()
	defer pg.ph.RUnlock()
	return pg.ph.ID
}

func (pg *PartyGroup) GetLeader() *rtapi.UserPresence {
	p := pg.ph
	p.RLock()
	defer p.RUnlock()
	if p.leader == nil {
		return nil
	}
	return p.leader.UserPresence
}

func (pg *PartyGroup) GetMembers() []*PartyPresenceListItem {
	p := pg.ph
	p.RLock()
	defer p.RUnlock()
	return p.members.List()
}

func (pg *PartyGroup) GetPresences() []rtapi.UserPresence {
	presences := make([]rtapi.UserPresence, 0, len(pg.GetMembers()))
	for _, presence := range pg.GetMembers() {
		if presence == nil {
			continue
		}
		return append(presences, *presence.UserPresence)
	}
	return presences
}

func (pg *PartyGroup) IsLeader() bool {
	p := pg.ph
	p.RLock()
	defer p.RUnlock()
	return p.leader != nil && pg.self != nil && p.leader.UserPresence.SessionId == pg.self.SessionId
}

func (pg *PartyGroup) Size() int {
	if pg.ph == nil || pg.ph.members == nil {
		return 0
	}
	pg.ph.RLock()
	defer pg.ph.RUnlock()
	return pg.ph.members.Size()
}

func JoinOrCreatePartyGroup(session *sessionWS, groupName string) (*PartyGroup, error) {

	partyID := uuid.NewV5(uuid.Nil, groupName)

	presence := &rtapi.UserPresence{
		UserId:    session.userID.String(),
		SessionId: session.id.String(),
		Username:  session.Username(),
	}

	partyRegistry := session.pipeline.partyRegistry.(*LocalPartyRegistry)
	node := session.pipeline.node

	ph, ok := partyRegistry.parties.Load(partyID)
	if ok {
		// Party already exists
		// Check if this user is already a member of the party.
		ph.RLock()
		for _, presence := range ph.members.List() {
			if presence.UserPresence.SessionId == session.id.String() {
				ph.RUnlock()
				return nil, status.Errorf(codes.AlreadyExists, "Already a member of the party")
			}
		}
		ph.RUnlock()

		_, err := partyRegistry.PartyJoinRequest(session.Context(), partyID, session.pipeline.node, &Presence{
			ID: PresenceID{
				Node:      session.pipeline.node,
				SessionID: session.ID(),
			},
			// Presence stream not needed.
			UserID: session.UserID(),
			Meta: PresenceMeta{
				Username: session.Username(),
				// Other meta fields not needed.
			},
		})
		switch err {
		case nil:

		case runtime.ErrPartyFull, runtime.ErrPartyJoinRequestsFull:
			return nil, status.Errorf(codes.ResourceExhausted, "Party is full")
		case runtime.ErrPartyJoinRequestDuplicate:
			return nil, status.Errorf(codes.AlreadyExists, "Duplicate join request")
		case runtime.ErrPartyJoinRequestAlreadyMember:
			// This is not an error, just a no-op.
		}
	} else {
		// Create the party
		maxSize := 8
		open := true

		ph = NewPartyHandler(partyRegistry.logger, partyRegistry, partyRegistry.matchmaker, partyRegistry.tracker, partyRegistry.streamManager, partyRegistry.router, partyID, partyRegistry.node, open, maxSize, presence)
		partyRegistry.parties.Store(partyID, ph)

		// If successful, the creator becomes the first user to join the party.
		success := session.pipeline.tracker.Update(session.Context(), session.ID(), ph.Stream, session.UserID(), PresenceMeta{
			Format:   session.Format(),
			Username: session.Username(),
			Status:   "",
		})
		if !success {
			_ = session.Send(&rtapi.Envelope{Message: &rtapi.Envelope_Error{Error: &rtapi.Error{
				Code:    int32(rtapi.Error_RUNTIME_EXCEPTION),
				Message: "Error tracking party creation",
			}}}, true)
			return nil, status.Errorf(codes.Internal, "Failed to track party creation")
		}

		out := &rtapi.Envelope{Message: &rtapi.Envelope_Party{Party: &rtapi.Party{
			PartyId:   ph.IDStr,
			Open:      open,
			MaxSize:   int32(maxSize),
			Self:      presence,
			Leader:    presence,
			Presences: []*rtapi.UserPresence{presence},
		}}}
		_ = session.Send(out, true)

	}

	// Track the party stream
	success := session.pipeline.tracker.Update(session.Context(), session.ID(), PresenceStream{Mode: StreamModeParty, Subject: partyID, Label: node}, session.UserID(), PresenceMeta{
		Format:   session.Format(),
		Username: session.Username(),
		Status:   "",
	})
	if !success {
		return nil, status.Errorf(codes.Internal, "Failed to track party join")
	}
	return &PartyGroup{
		name: groupName,
		ph:   ph,
		self: presence,
	}, nil
}
