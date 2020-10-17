package repo

import (
	"context"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/getcouragenow/sys-share/sys-account/service/go/pkg"

	"github.com/getcouragenow/sys/sys-account/service/go/pkg/auth"
	"github.com/getcouragenow/sys/sys-account/service/go/pkg/dao"
	coredb "github.com/getcouragenow/sys/sys-core/service/go/pkg/db"
)

func (ad *SysAccountRepo) NewAccount(ctx context.Context, in *pkg.Account) (*pkg.Account, error) {
	now := timestampNow()
	roleId := coredb.UID()
	if err := ad.store.InsertRole(&dao.Role{
		ID:        roleId,
		AccountId: in.Id, // TODO @gutterbacon check for uniqueness
		Role:      int(in.Role.Role),
		ProjectId: in.Role.ProjectID,
		OrgId:     in.Role.OrgID,
		CreatedAt: now,
	}); err != nil {
		return nil, err
	}
	if err := ad.store.InsertAccount(&dao.Account{
		ID:                in.Id,
		Email:             in.Email,
		Password:          in.Password,
		RoleId:            roleId,
		UserDefinedFields: in.Fields.Fields,
		CreatedAt:         in.CreatedAt,
		UpdatedAt:         in.UpdatedAt,
		LastLogin:         in.LastLogin,
		Disabled:          in.Disabled,
	}); err != nil {
		return nil, err
	}
	acc, err := ad.store.GetAccount(&dao.QueryParams{Params: map[string]interface{}{"id": in.Id}})
	if err != nil {
		return nil, err
	}
	role, err := ad.store.GetRole(&dao.QueryParams{Params: map[string]interface{}{"id": acc.RoleId}})
	if err != nil {
		return nil, err
	}
	userRole, err := role.ToPkgRole()
	if err != nil {
		return nil, err
	}
	return acc.ToPkgAccount(userRole)
}

func (ad *SysAccountRepo) GetAccount(ctx context.Context, in *pkg.GetAccountRequest) (*pkg.Account, error) {
	if in == nil {
		return &pkg.Account{},
			status.Errorf(codes.InvalidArgument, "cannot get user account: %v", auth.Error{Reason: auth.ErrInvalidParameters})
	}

	// TODO @gutterbacon: In the absence of actual enforcement policy function, this method is a stub. We allow everyone to query anything at this point.
	acc, err := ad.store.GetAccount(&dao.QueryParams{Params: map[string]interface{}{
		"id": in.Id,
	}})
	if err != nil {
		return nil, status.Errorf(codes.NotFound, "cannot find user account: %v", auth.Error{Reason: auth.ErrAccountNotFound})
	}
	role, err := ad.store.GetRole(&dao.QueryParams{Params: map[string]interface{}{
		"id": acc.RoleId,
	}})
	if err != nil {
		return nil, status.Errorf(codes.NotFound, "cannot find user role: %v", auth.Error{Reason: auth.ErrAccountNotFound})
	}
	userRole, err := role.ToPkgRole()
	if err != nil {
		return nil, status.Errorf(codes.NotFound, "cannot find user role: %v", auth.Error{Reason: auth.ErrAccountNotFound})
	}

	return acc.ToPkgAccount(userRole)
}

// TODO @gutterbacon: In the absence of actual enforcement policy function, this method is a stub. We allow everyone to query anything at this point.
func (ad *SysAccountRepo) ListAccounts(ctx context.Context, in *pkg.ListAccountsRequest) (*pkg.ListAccountsResponse, error) {
	if in == nil {
		return &pkg.ListAccountsResponse{}, status.Errorf(codes.InvalidArgument, "cannot list user accounts: %v", auth.Error{Reason: auth.ErrInvalidParameters})
	}
	return &pkg.ListAccountsResponse{}, nil
}

// TODO @gutterbacon: In the absence of actual enforcement policy function, this method is a stub. We allow everyone to query anything at this point.
func (ad *SysAccountRepo) SearchAccounts(ctx context.Context, in *pkg.SearchAccountsRequest) (*pkg.SearchAccountsResponse, error) {
	return &pkg.SearchAccountsResponse{}, nil
}

// TODO @gutterbacon: In the absence of actual enforcement policy function, this method is a stub. We allow everyone to query anything at this point.
func (ad *SysAccountRepo) AssignAccountToRole(context.Context, *pkg.AssignAccountToRoleRequest) (*pkg.Account, error) {
	return &pkg.Account{}, nil
}

// TODO @gutterbacon: In the absence of actual enforcement policy function, this method is a stub. We allow everyone to query anything at this point.
func (ad *SysAccountRepo) UpdateAccount(context.Context, *pkg.Account) (*pkg.Account, error) {
	return &pkg.Account{}, nil
}

// TODO @gutterbacon: In the absence of actual enforcement policy function, this method is a stub. We allow everyone to query anything at this point.
func (ad *SysAccountRepo) DisableAccount(context.Context, *pkg.DisableAccountRequest) (*pkg.Account, error) {
	return &pkg.Account{}, nil
}