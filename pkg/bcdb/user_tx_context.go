package bcdb

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"

	"github.com/golang/protobuf/proto"

	"github.ibm.com/blockchaindb/server/pkg/constants"
	"github.ibm.com/blockchaindb/server/pkg/cryptoservice"
	"github.ibm.com/blockchaindb/server/pkg/types"
)

// UsersTxContext transaction context to operate with
// user management related transactions:
// 1. Add user's record
// 2. Get user's record
// 3. Delete user's record
// 4. Alternate user's ACLs
type UsersTxContext interface {
	// Embed general abstraction
	TxContext
	// PutUser introduce new user into database
	PutUser(user *types.User, acl *types.AccessControl) error
	// GetUser obtain user's record from database
	GetUser(userID string) (*types.User, error)
	// RemoveUser delete existing user from the database
	RemoveUser(userID string) error
}

type userTxContext struct {
	commonTxContext
	userReads   []*types.UserRead
	userWrites  []*types.UserWrite
	userDeletes []*types.UserDelete
}

func (u *userTxContext) Commit() (string, error) {
	return u.commit(u, constants.PostUserTx)
}

func (u *userTxContext) Abort() error {
	return u.abort(u)
}

func (u *userTxContext) PutUser(user *types.User, acl *types.AccessControl) error {
	// TODO: decide whenever we going to support read your own writes
	u.userWrites = append(u.userWrites, &types.UserWrite{
		User: user,
		ACL:  acl,
	})
	return nil
}

func (u *userTxContext) GetUser(userID string) (*types.User, error) {
	getUser := &url.URL{
		Path: constants.URLForGetUser(userID),
	}
	replica := u.selectReplica()
	configREST := replica.ResolveReference(getUser)

	ctx := context.TODO() // TODO: Replace with timeout
	response, err := u.restClient.Query(ctx, configREST.String(), &types.GetUserQuery{
		UserID:       u.userID,
		TargetUserID: userID,
	})
	if err != nil {
		u.logger.Errorf("failed to send query transaction to obtain record for userID = %s, due to %s", userID, err)
		return nil, err
	}

	if response.StatusCode != http.StatusOK {
		u.logger.Errorf("error getting user's record, server returned %s", response.Status)
		return nil, errors.New(fmt.Sprintf("error getting user's record, server returned %s", response.Status))
	}

	res := &types.GetUserResponseEnvelope{}
	err = json.NewDecoder(response.Body).Decode(res)
	if err != nil {
		u.logger.Errorf("failed to decode json response, due to", err)
		return nil, err
	}

	u.userReads = append(u.userReads, &types.UserRead{
		UserID:  userID,
		Version: res.GetPayload().GetMetadata().GetVersion(),
	})

	return res.GetPayload().GetUser(), nil
}

func (u *userTxContext) RemoveUser(userID string) error {
	u.userDeletes = append(u.userDeletes, &types.UserDelete{
		UserID: userID,
	})
	return nil
}

func (u *userTxContext) composeEnvelope(txID string) (proto.Message, error) {
	payload := &types.UserAdministrationTx{
		UserID:      u.userID,
		TxID:        txID,
		UserReads:   u.userReads,
		UserWrites:  u.userWrites,
		UserDeletes: u.userDeletes,
	}

	signature, err := cryptoservice.SignTx(u.signer, payload)
	if err != nil {
		return nil, err
	}

	return &types.UserAdministrationTxEnvelope{
		Payload:   payload,
		Signature: signature,
	}, nil
}

func (u *userTxContext) cleanCtx() {
	u.userDeletes = []*types.UserDelete{}
	u.userWrites = []*types.UserWrite{}
	u.userReads = []*types.UserRead{}
}
