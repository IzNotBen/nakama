package server

import (
	"context"
	"database/sql"

	"github.com/heroiclabs/nakama-common/api"
	"github.com/heroiclabs/nakama-common/rtapi"
	"github.com/heroiclabs/nakama-common/runtime"
)

func registerGuard(initializer runtime.Initializer) error {
	rtMessages := []string{
		"ChannelJoin",
		"ChannelLeave",
		"ChannelMessageSend",
		"ChannelMessageUpdate",
		"ChannelMessageRemove",
		"MatchCreate",
		"MatchDataSend",
		"MatchJoin",
		"MatchLeave",
		"MatchmakerAdd",
		"MatchmakerRemove",
		"PartyCreate",
		"PartyCreate",
		"PartyJoin",
		"PartyLeave",
		"PartyPromote",
		"PartyAccept",
		"PartyRemove",
		"PartyClose",
		"PartyJoinRequestList",
		"PartyMatchmakerAdd",
		"PartyMatchmakerRemove",
		"PartyDataSend",
		//"Ping",
		//"Pong",
		"Rpc",
		"StatusFollow",
		"StatusUnfollow",
		"StatusUpdate",
	}
	for _, rtMessage := range rtMessages {
		if err := initializer.RegisterBeforeRt(rtMessage, func(ctx context.Context, logger runtime.Logger, db *sql.DB, nk runtime.NakamaModule, envelope *rtapi.Envelope) (*rtapi.Envelope, error) {
			return nil, nil
		}); err != nil {
			return err
		}
	}

	if err := initializer.RegisterBeforeAddFriends(func(ctx context.Context, logger runtime.Logger, db *sql.DB, nk runtime.NakamaModule, in *api.AddFriendsRequest) (*api.AddFriendsRequest, error) {
		return nil, nil
	}); err != nil {
		return err
	}
	if err := initializer.RegisterBeforeAddGroupUsers(func(ctx context.Context, logger runtime.Logger, db *sql.DB, nk runtime.NakamaModule, in *api.AddGroupUsersRequest) (*api.AddGroupUsersRequest, error) {
		return nil, nil
	}); err != nil {
		return err
	}
	if err := initializer.RegisterBeforeAuthenticateApple(func(ctx context.Context, logger runtime.Logger, db *sql.DB, nk runtime.NakamaModule, in *api.AuthenticateAppleRequest) (*api.AuthenticateAppleRequest, error) {
		return nil, nil
	}); err != nil {
		return err
	}
	if err := initializer.RegisterBeforeAuthenticateCustom(func(ctx context.Context, logger runtime.Logger, db *sql.DB, nk runtime.NakamaModule, in *api.AuthenticateCustomRequest) (*api.AuthenticateCustomRequest, error) {
		return nil, nil
	}); err != nil {
		return err
	}

	/*
		if err := initializer.RegisterBeforeAuthenticateDevice(func(ctx context.Context, logger runtime.Logger, db *sql.DB, nk runtime.NakamaModule, in *api.AuthenticateDeviceRequest) (*api.AuthenticateDeviceRequest, error) {
			return nil, nil
		}); err != nil {
			return err
		}
	*/

	/*
		if err := initializer.RegisterBeforeAuthenticateEmail(func(ctx context.Context, logger runtime.Logger, db *sql.DB, nk runtime.NakamaModule, in *api.AuthenticateEmailRequest) (*api.AuthenticateEmailRequest, error) {
			return nil, nil
		}); err != nil {
			return err
		}
	*/

	if err := initializer.RegisterBeforeAuthenticateFacebook(func(ctx context.Context, logger runtime.Logger, db *sql.DB, nk runtime.NakamaModule, in *api.AuthenticateFacebookRequest) (*api.AuthenticateFacebookRequest, error) {
		return nil, nil
	}); err != nil {
		return err
	}
	if err := initializer.RegisterBeforeAuthenticateFacebookInstantGame(func(ctx context.Context, logger runtime.Logger, db *sql.DB, nk runtime.NakamaModule, in *api.AuthenticateFacebookInstantGameRequest) (*api.AuthenticateFacebookInstantGameRequest, error) {
		return nil, nil
	}); err != nil {
		return err
	}
	if err := initializer.RegisterBeforeAuthenticateGameCenter(func(ctx context.Context, logger runtime.Logger, db *sql.DB, nk runtime.NakamaModule, in *api.AuthenticateGameCenterRequest) (*api.AuthenticateGameCenterRequest, error) {
		return nil, nil
	}); err != nil {
		return err
	}
	if err := initializer.RegisterBeforeAuthenticateGoogle(func(ctx context.Context, logger runtime.Logger, db *sql.DB, nk runtime.NakamaModule, in *api.AuthenticateGoogleRequest) (*api.AuthenticateGoogleRequest, error) {
		return nil, nil
	}); err != nil {
		return err
	}
	if err := initializer.RegisterBeforeAuthenticateSteam(func(ctx context.Context, logger runtime.Logger, db *sql.DB, nk runtime.NakamaModule, in *api.AuthenticateSteamRequest) (*api.AuthenticateSteamRequest, error) {
		return nil, nil
	}); err != nil {
		return err
	}
	if err := initializer.RegisterBeforeBanGroupUsers(func(ctx context.Context, logger runtime.Logger, db *sql.DB, nk runtime.NakamaModule, in *api.BanGroupUsersRequest) (*api.BanGroupUsersRequest, error) {
		return nil, nil
	}); err != nil {
		return err
	}
	if err := initializer.RegisterBeforeBlockFriends(func(ctx context.Context, logger runtime.Logger, db *sql.DB, nk runtime.NakamaModule, in *api.BlockFriendsRequest) (*api.BlockFriendsRequest, error) {
		return nil, nil
	}); err != nil {
		return err
	}
	if err := initializer.RegisterBeforeCreateGroup(func(ctx context.Context, logger runtime.Logger, db *sql.DB, nk runtime.NakamaModule, in *api.CreateGroupRequest) (*api.CreateGroupRequest, error) {
		return nil, nil
	}); err != nil {
		return err
	}
	/*
		if err := initializer.RegisterBeforeDeleteAccount(func(ctx context.Context, logger runtime.Logger, db *sql.DB, nk runtime.NakamaModule) error {
			return nil
		}); err != nil {
			return err
		}
	*/

	if err := initializer.RegisterBeforeDeleteFriends(func(ctx context.Context, logger runtime.Logger, db *sql.DB, nk runtime.NakamaModule, in *api.DeleteFriendsRequest) (*api.DeleteFriendsRequest, error) {
		return nil, nil
	}); err != nil {
		return err
	}
	if err := initializer.RegisterBeforeDeleteGroup(func(ctx context.Context, logger runtime.Logger, db *sql.DB, nk runtime.NakamaModule, in *api.DeleteGroupRequest) (*api.DeleteGroupRequest, error) {
		return nil, nil
	}); err != nil {
		return err
	}
	if err := initializer.RegisterBeforeDeleteLeaderboardRecord(func(ctx context.Context, logger runtime.Logger, db *sql.DB, nk runtime.NakamaModule, in *api.DeleteLeaderboardRecordRequest) (*api.DeleteLeaderboardRecordRequest, error) {
		return nil, nil
	}); err != nil {
		return err
	}
	if err := initializer.RegisterBeforeDeleteNotifications(func(ctx context.Context, logger runtime.Logger, db *sql.DB, nk runtime.NakamaModule, in *api.DeleteNotificationsRequest) (*api.DeleteNotificationsRequest, error) {
		return nil, nil
	}); err != nil {
		return err
	}
	if err := initializer.RegisterBeforeDeleteStorageObjects(func(ctx context.Context, logger runtime.Logger, db *sql.DB, nk runtime.NakamaModule, in *api.DeleteStorageObjectsRequest) (*api.DeleteStorageObjectsRequest, error) {
		return nil, nil
	}); err != nil {
		return err
	}
	if err := initializer.RegisterBeforeDeleteTournamentRecord(func(ctx context.Context, logger runtime.Logger, db *sql.DB, nk runtime.NakamaModule, in *api.DeleteTournamentRecordRequest) (*api.DeleteTournamentRecordRequest, error) {
		return nil, nil
	}); err != nil {
		return err
	}
	if err := initializer.RegisterBeforeDemoteGroupUsers(func(ctx context.Context, logger runtime.Logger, db *sql.DB, nk runtime.NakamaModule, in *api.DemoteGroupUsersRequest) (*api.DemoteGroupUsersRequest, error) {
		return nil, nil
	}); err != nil {
		return err
	}

	/*
		if err := initializer.RegisterBeforeGetAccount(func(ctx context.Context, logger runtime.Logger, db *sql.DB, nk runtime.NakamaModule) error {
			return nil
		}); err != nil {
			return err
		}
	*/

	if err := initializer.RegisterBeforeGetSubscription(func(ctx context.Context, logger runtime.Logger, db *sql.DB, nk runtime.NakamaModule, in *api.GetSubscriptionRequest) (*api.GetSubscriptionRequest, error) {
		return nil, nil
	}); err != nil {
		return err
	}
	if err := initializer.RegisterBeforeGetUsers(func(ctx context.Context, logger runtime.Logger, db *sql.DB, nk runtime.NakamaModule, in *api.GetUsersRequest) (*api.GetUsersRequest, error) {
		return nil, nil
	}); err != nil {
		return err
	}
	if err := initializer.RegisterBeforeImportFacebookFriends(func(ctx context.Context, logger runtime.Logger, db *sql.DB, nk runtime.NakamaModule, in *api.ImportFacebookFriendsRequest) (*api.ImportFacebookFriendsRequest, error) {
		return nil, nil
	}); err != nil {
		return err
	}
	if err := initializer.RegisterBeforeImportSteamFriends(func(ctx context.Context, logger runtime.Logger, db *sql.DB, nk runtime.NakamaModule, in *api.ImportSteamFriendsRequest) (*api.ImportSteamFriendsRequest, error) {
		return nil, nil
	}); err != nil {
		return err
	}
	if err := initializer.RegisterBeforeJoinGroup(func(ctx context.Context, logger runtime.Logger, db *sql.DB, nk runtime.NakamaModule, in *api.JoinGroupRequest) (*api.JoinGroupRequest, error) {
		return nil, nil
	}); err != nil {
		return err
	}
	if err := initializer.RegisterBeforeJoinTournament(func(ctx context.Context, logger runtime.Logger, db *sql.DB, nk runtime.NakamaModule, in *api.JoinTournamentRequest) (*api.JoinTournamentRequest, error) {
		return nil, nil
	}); err != nil {
		return err
	}
	if err := initializer.RegisterBeforeKickGroupUsers(func(ctx context.Context, logger runtime.Logger, db *sql.DB, nk runtime.NakamaModule, in *api.KickGroupUsersRequest) (*api.KickGroupUsersRequest, error) {
		return nil, nil
	}); err != nil {
		return err
	}
	if err := initializer.RegisterBeforeLeaveGroup(func(ctx context.Context, logger runtime.Logger, db *sql.DB, nk runtime.NakamaModule, in *api.LeaveGroupRequest) (*api.LeaveGroupRequest, error) {
		return nil, nil
	}); err != nil {
		return err
	}
	if err := initializer.RegisterBeforeLinkApple(func(ctx context.Context, logger runtime.Logger, db *sql.DB, nk runtime.NakamaModule, in *api.AccountApple) (*api.AccountApple, error) {
		return nil, nil
	}); err != nil {
		return err
	}
	if err := initializer.RegisterBeforeLinkCustom(func(ctx context.Context, logger runtime.Logger, db *sql.DB, nk runtime.NakamaModule, in *api.AccountCustom) (*api.AccountCustom, error) {
		return nil, nil
	}); err != nil {
		return err
	}
	if err := initializer.RegisterBeforeLinkDevice(func(ctx context.Context, logger runtime.Logger, db *sql.DB, nk runtime.NakamaModule, in *api.AccountDevice) (*api.AccountDevice, error) {
		return nil, nil
	}); err != nil {
		return err
	}
	if err := initializer.RegisterBeforeLinkEmail(func(ctx context.Context, logger runtime.Logger, db *sql.DB, nk runtime.NakamaModule, in *api.AccountEmail) (*api.AccountEmail, error) {
		return nil, nil
	}); err != nil {
		return err
	}
	if err := initializer.RegisterBeforeLinkFacebook(func(ctx context.Context, logger runtime.Logger, db *sql.DB, nk runtime.NakamaModule, in *api.LinkFacebookRequest) (*api.LinkFacebookRequest, error) {
		return nil, nil
	}); err != nil {
		return err
	}
	if err := initializer.RegisterBeforeLinkFacebookInstantGame(func(ctx context.Context, logger runtime.Logger, db *sql.DB, nk runtime.NakamaModule, in *api.AccountFacebookInstantGame) (*api.AccountFacebookInstantGame, error) {
		return nil, nil
	}); err != nil {
		return err
	}
	if err := initializer.RegisterBeforeLinkGameCenter(func(ctx context.Context, logger runtime.Logger, db *sql.DB, nk runtime.NakamaModule, in *api.AccountGameCenter) (*api.AccountGameCenter, error) {
		return nil, nil
	}); err != nil {
		return err
	}
	if err := initializer.RegisterBeforeLinkGoogle(func(ctx context.Context, logger runtime.Logger, db *sql.DB, nk runtime.NakamaModule, in *api.AccountGoogle) (*api.AccountGoogle, error) {
		return nil, nil
	}); err != nil {
		return err
	}
	if err := initializer.RegisterBeforeLinkSteam(func(ctx context.Context, logger runtime.Logger, db *sql.DB, nk runtime.NakamaModule, in *api.LinkSteamRequest) (*api.LinkSteamRequest, error) {
		return nil, nil
	}); err != nil {
		return err
	}
	if err := initializer.RegisterBeforeListChannelMessages(func(ctx context.Context, logger runtime.Logger, db *sql.DB, nk runtime.NakamaModule, in *api.ListChannelMessagesRequest) (*api.ListChannelMessagesRequest, error) {
		return nil, nil
	}); err != nil {
		return err
	}
	if err := initializer.RegisterBeforeListFriends(func(ctx context.Context, logger runtime.Logger, db *sql.DB, nk runtime.NakamaModule, in *api.ListFriendsRequest) (*api.ListFriendsRequest, error) {
		return nil, nil
	}); err != nil {
		return err
	}
	if err := initializer.RegisterBeforeListGroupUsers(func(ctx context.Context, logger runtime.Logger, db *sql.DB, nk runtime.NakamaModule, in *api.ListGroupUsersRequest) (*api.ListGroupUsersRequest, error) {
		return nil, nil
	}); err != nil {
		return err
	}
	if err := initializer.RegisterBeforeListGroups(func(ctx context.Context, logger runtime.Logger, db *sql.DB, nk runtime.NakamaModule, in *api.ListGroupsRequest) (*api.ListGroupsRequest, error) {
		return nil, nil
	}); err != nil {
		return err
	}
	if err := initializer.RegisterBeforeListLeaderboardRecords(func(ctx context.Context, logger runtime.Logger, db *sql.DB, nk runtime.NakamaModule, in *api.ListLeaderboardRecordsRequest) (*api.ListLeaderboardRecordsRequest, error) {
		return nil, nil
	}); err != nil {
		return err
	}
	if err := initializer.RegisterBeforeListLeaderboardRecordsAroundOwner(func(ctx context.Context, logger runtime.Logger, db *sql.DB, nk runtime.NakamaModule, in *api.ListLeaderboardRecordsAroundOwnerRequest) (*api.ListLeaderboardRecordsAroundOwnerRequest, error) {
		return nil, nil
	}); err != nil {
		return err
	}
	if err := initializer.RegisterBeforeListMatches(func(ctx context.Context, logger runtime.Logger, db *sql.DB, nk runtime.NakamaModule, in *api.ListMatchesRequest) (*api.ListMatchesRequest, error) {
		return nil, nil
	}); err != nil {
		return err
	}
	if err := initializer.RegisterBeforeListNotifications(func(ctx context.Context, logger runtime.Logger, db *sql.DB, nk runtime.NakamaModule, in *api.ListNotificationsRequest) (*api.ListNotificationsRequest, error) {
		return nil, nil
	}); err != nil {
		return err
	}
	if err := initializer.RegisterBeforeListStorageObjects(func(ctx context.Context, logger runtime.Logger, db *sql.DB, nk runtime.NakamaModule, in *api.ListStorageObjectsRequest) (*api.ListStorageObjectsRequest, error) {
		return nil, nil
	}); err != nil {
		return err
	}
	if err := initializer.RegisterBeforeListSubscriptions(func(ctx context.Context, logger runtime.Logger, db *sql.DB, nk runtime.NakamaModule, in *api.ListSubscriptionsRequest) (*api.ListSubscriptionsRequest, error) {
		return nil, nil
	}); err != nil {
		return err
	}
	if err := initializer.RegisterBeforeListTournamentRecords(func(ctx context.Context, logger runtime.Logger, db *sql.DB, nk runtime.NakamaModule, in *api.ListTournamentRecordsRequest) (*api.ListTournamentRecordsRequest, error) {
		return nil, nil
	}); err != nil {
		return err
	}
	if err := initializer.RegisterBeforeListTournamentRecordsAroundOwner(func(ctx context.Context, logger runtime.Logger, db *sql.DB, nk runtime.NakamaModule, in *api.ListTournamentRecordsAroundOwnerRequest) (*api.ListTournamentRecordsAroundOwnerRequest, error) {
		return nil, nil
	}); err != nil {
		return err
	}
	if err := initializer.RegisterBeforeListTournaments(func(ctx context.Context, logger runtime.Logger, db *sql.DB, nk runtime.NakamaModule, in *api.ListTournamentsRequest) (*api.ListTournamentsRequest, error) {
		return nil, nil
	}); err != nil {
		return err
	}
	if err := initializer.RegisterBeforeListUserGroups(func(ctx context.Context, logger runtime.Logger, db *sql.DB, nk runtime.NakamaModule, in *api.ListUserGroupsRequest) (*api.ListUserGroupsRequest, error) {
		return nil, nil
	}); err != nil {
		return err
	}
	if err := initializer.RegisterBeforePromoteGroupUsers(func(ctx context.Context, logger runtime.Logger, db *sql.DB, nk runtime.NakamaModule, in *api.PromoteGroupUsersRequest) (*api.PromoteGroupUsersRequest, error) {
		return nil, nil
	}); err != nil {
		return err
	}
	if err := initializer.RegisterBeforeReadStorageObjects(func(ctx context.Context, logger runtime.Logger, db *sql.DB, nk runtime.NakamaModule, in *api.ReadStorageObjectsRequest) (*api.ReadStorageObjectsRequest, error) {
		return nil, nil
	}); err != nil {
		return err
	}
	//if err := initializer.RegisterBeforeSessionLogout(func(ctx context.Context, logger runtime.Logger, db *sql.DB, nk runtime.NakamaModule, in *api.SessionLogoutRequest) (*api.SessionLogoutRequest, error) {
	//	return nil, nil
	//}); err != nil {
	//	return err
	//}
	//if err := initializer.RegisterBeforeSessionRefresh(func(ctx context.Context, logger runtime.Logger, db *sql.DB, nk runtime.NakamaModule, in *api.SessionRefreshRequest) (*api.SessionRefreshRequest, error) {
	//	return nil, nil
	//}); err != nil {
	//	return err
	//}
	if err := initializer.RegisterBeforeUnlinkApple(func(ctx context.Context, logger runtime.Logger, db *sql.DB, nk runtime.NakamaModule, in *api.AccountApple) (*api.AccountApple, error) {
		return nil, nil
	}); err != nil {
		return err
	}
	if err := initializer.RegisterBeforeUnlinkCustom(func(ctx context.Context, logger runtime.Logger, db *sql.DB, nk runtime.NakamaModule, in *api.AccountCustom) (*api.AccountCustom, error) {
		return nil, nil
	}); err != nil {
		return err
	}
	if err := initializer.RegisterBeforeUnlinkDevice(func(ctx context.Context, logger runtime.Logger, db *sql.DB, nk runtime.NakamaModule, in *api.AccountDevice) (*api.AccountDevice, error) {
		return nil, nil
	}); err != nil {
		return err
	}
	if err := initializer.RegisterBeforeUnlinkEmail(func(ctx context.Context, logger runtime.Logger, db *sql.DB, nk runtime.NakamaModule, in *api.AccountEmail) (*api.AccountEmail, error) {
		return nil, nil
	}); err != nil {
		return err
	}
	if err := initializer.RegisterBeforeUnlinkFacebook(func(ctx context.Context, logger runtime.Logger, db *sql.DB, nk runtime.NakamaModule, in *api.AccountFacebook) (*api.AccountFacebook, error) {
		return nil, nil
	}); err != nil {
		return err
	}
	if err := initializer.RegisterBeforeUnlinkFacebookInstantGame(func(ctx context.Context, logger runtime.Logger, db *sql.DB, nk runtime.NakamaModule, in *api.AccountFacebookInstantGame) (*api.AccountFacebookInstantGame, error) {
		return nil, nil
	}); err != nil {
		return err
	}
	if err := initializer.RegisterBeforeUnlinkGameCenter(func(ctx context.Context, logger runtime.Logger, db *sql.DB, nk runtime.NakamaModule, in *api.AccountGameCenter) (*api.AccountGameCenter, error) {
		return nil, nil
	}); err != nil {
		return err
	}
	if err := initializer.RegisterBeforeUnlinkGoogle(func(ctx context.Context, logger runtime.Logger, db *sql.DB, nk runtime.NakamaModule, in *api.AccountGoogle) (*api.AccountGoogle, error) {
		return nil, nil
	}); err != nil {
		return err
	}
	if err := initializer.RegisterBeforeUnlinkSteam(func(ctx context.Context, logger runtime.Logger, db *sql.DB, nk runtime.NakamaModule, in *api.AccountSteam) (*api.AccountSteam, error) {
		return nil, nil
	}); err != nil {
		return err
	}
	if err := initializer.RegisterBeforeUpdateAccount(func(ctx context.Context, logger runtime.Logger, db *sql.DB, nk runtime.NakamaModule, in *api.UpdateAccountRequest) (*api.UpdateAccountRequest, error) {
		return nil, nil
	}); err != nil {
		return err
	}
	if err := initializer.RegisterBeforeUpdateGroup(func(ctx context.Context, logger runtime.Logger, db *sql.DB, nk runtime.NakamaModule, in *api.UpdateGroupRequest) (*api.UpdateGroupRequest, error) {
		return nil, nil
	}); err != nil {
		return err
	}
	if err := initializer.RegisterBeforeValidatePurchaseApple(func(ctx context.Context, logger runtime.Logger, db *sql.DB, nk runtime.NakamaModule, in *api.ValidatePurchaseAppleRequest) (*api.ValidatePurchaseAppleRequest, error) {
		return nil, nil
	}); err != nil {
		return err
	}
	if err := initializer.RegisterBeforeValidatePurchaseFacebookInstant(func(ctx context.Context, logger runtime.Logger, db *sql.DB, nk runtime.NakamaModule, in *api.ValidatePurchaseFacebookInstantRequest) (*api.ValidatePurchaseFacebookInstantRequest, error) {
		return nil, nil
	}); err != nil {
		return err
	}
	if err := initializer.RegisterBeforeValidatePurchaseGoogle(func(ctx context.Context, logger runtime.Logger, db *sql.DB, nk runtime.NakamaModule, in *api.ValidatePurchaseGoogleRequest) (*api.ValidatePurchaseGoogleRequest, error) {
		return nil, nil
	}); err != nil {
		return err
	}
	if err := initializer.RegisterBeforeValidatePurchaseHuawei(func(ctx context.Context, logger runtime.Logger, db *sql.DB, nk runtime.NakamaModule, in *api.ValidatePurchaseHuaweiRequest) (*api.ValidatePurchaseHuaweiRequest, error) {
		return nil, nil
	}); err != nil {
		return err
	}
	if err := initializer.RegisterBeforeValidateSubscriptionApple(func(ctx context.Context, logger runtime.Logger, db *sql.DB, nk runtime.NakamaModule, in *api.ValidateSubscriptionAppleRequest) (*api.ValidateSubscriptionAppleRequest, error) {
		return nil, nil
	}); err != nil {
		return err
	}
	if err := initializer.RegisterBeforeValidateSubscriptionGoogle(func(ctx context.Context, logger runtime.Logger, db *sql.DB, nk runtime.NakamaModule, in *api.ValidateSubscriptionGoogleRequest) (*api.ValidateSubscriptionGoogleRequest, error) {
		return nil, nil
	}); err != nil {
		return err
	}
	if err := initializer.RegisterBeforeWriteLeaderboardRecord(func(ctx context.Context, logger runtime.Logger, db *sql.DB, nk runtime.NakamaModule, in *api.WriteLeaderboardRecordRequest) (*api.WriteLeaderboardRecordRequest, error) {
		return nil, nil
	}); err != nil {
		return err
	}
	if err := initializer.RegisterBeforeWriteStorageObjects(func(ctx context.Context, logger runtime.Logger, db *sql.DB, nk runtime.NakamaModule, in *api.WriteStorageObjectsRequest) (*api.WriteStorageObjectsRequest, error) {
		return nil, nil
	}); err != nil {
		return err
	}
	if err := initializer.RegisterBeforeWriteTournamentRecord(func(ctx context.Context, logger runtime.Logger, db *sql.DB, nk runtime.NakamaModule, in *api.WriteTournamentRecordRequest) (*api.WriteTournamentRecordRequest, error) {
		return nil, nil
	}); err != nil {
		return err
	}

	return nil
}
