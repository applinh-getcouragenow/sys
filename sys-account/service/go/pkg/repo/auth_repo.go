package repo

import (
	"context"
	"fmt"
	corepkg "github.com/getcouragenow/sys-share/sys-core/service/go/pkg"
	"github.com/getcouragenow/sys/sys-account/service/go/pkg/dao"
	"google.golang.org/protobuf/types/known/emptypb"

	l "github.com/sirupsen/logrus"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/getcouragenow/sys-share/sys-account/service/go/pkg"
	sharedAuth "github.com/getcouragenow/sys-share/sys-account/service/go/pkg/shared"

	"github.com/getcouragenow/sys/sys-account/service/go/pkg/pass"
	coredb "github.com/getcouragenow/sys/sys-core/service/go/pkg/coredb"
)

func (ad *SysAccountRepo) getAndVerifyAccount(_ context.Context, req *pkg.LoginRequest) (*pkg.Account, error) {
	qp := &coredb.QueryParams{Params: map[string]interface{}{
		"email": req.Email,
	}}
	acc, err := ad.store.GetAccount(qp)
	if err != nil {
		return nil, err
	}

	if acc.Disabled {
		return nil, fmt.Errorf(sharedAuth.Error{Reason: sharedAuth.ErrAccountDisabled, Err: fmt.Errorf("password mismatch")}.Error())
	}

	matchedPassword, err := pass.VerifyHash(req.Password, acc.Password)
	if err != nil {
		return nil, err
	}
	if !matchedPassword {
		return nil, fmt.Errorf(sharedAuth.Error{Reason: sharedAuth.ErrVerifyPassword, Err: fmt.Errorf("password mismatch")}.Error())
	}

	ad.log.WithFields(l.Fields{
		"account_id": acc.ID,
	}).Debug("querying user")

	daoRoles, err := ad.store.FetchRoles(acc.ID)
	if err != nil {
		ad.log.Debugf("unable to fetch user roles: %v", err)
		return nil, err
	}
	var pkgRoles []*pkg.UserRoles
	for _, daoRole := range daoRoles {
		pkgRole, err := daoRole.ToPkgRole()
		if err != nil {
			ad.log.Debugf("unable to convert user role to pkg role: %v", err)
			return nil, err
		}
		pkgRoles = append(pkgRoles, pkgRole)
	}

	return acc.ToPkgAccount(pkgRoles)
}

// Register satisfies rpc.Register function on AuthService proto definition
func (ad *SysAccountRepo) Register(ctx context.Context, in *pkg.RegisterRequest) (*pkg.RegisterResponse, error) {
	if in == nil {
		return nil, status.Errorf(codes.InvalidArgument, "invalid argument")
	}
	if in.Password != in.PasswordConfirm {
		return nil, status.Errorf(codes.InvalidArgument, "password mismatch")
	}
	// New user will be assigned GUEST role and no Org / Project for now.
	// TODO @gutterbacon: subject to change.
	accountId := coredb.NewID()
	now := timestampNow()
	newAcc := &pkg.Account{
		Id:       accountId,
		Email:    in.Email,
		Password: in.Password,
		Role: []*pkg.UserRoles{
			{
				Role: 1,
				All:  false,
			},
		},
		CreatedAt: now,
		UpdatedAt: now,
		Disabled:  false,
		Fields:    &pkg.UserDefinedFields{},
		Survey:    &pkg.UserDefinedFields{},
		Verified:  false,
	}
	acc, err := ad.store.InsertFromPkgAccountRequest(newAcc)
	if err != nil {
		return &pkg.RegisterResponse{
			Success:     false,
			ErrorReason: err.Error(),
		}, err
	}

	vtoken, _, err := ad.genVerificationToken(&coredb.QueryParams{Params: map[string]interface{}{"email": in.Email}})
	if err != nil {
		return nil, err
	}

	errChan := make(chan error, 1)
	go func() {
		mailContent, err := ad.mailVerifyAccountTpl(acc.Email, vtoken)
		if err != nil {
			ad.log.Debugf("cannot create verify account email: %v", err)
			errChan <- err
			return
		}
		resp, err := ad.mail.SendMail(ctx, &corepkg.EmailRequest{
			Subject: fmt.Sprintf("Verify Account %s Register", acc.Email),
			Recipients: map[string]string{
				acc.Email: acc.Email,
			},
			Content: mailContent,
		})
		if err != nil {
			ad.log.Debugf("cannot send verify account email: %v", err)
			errChan <- err
			return
		}
		ad.log.Debugf("Sent Email to %s => %v\n", acc.Email, resp)
	}()
	if err = <-errChan; err != nil {
		ad.log.Errorf("Cannot send email: %v", err)
		// return nil, err
	}
	close(errChan)

	return &pkg.RegisterResponse{
		Success:     true,
		SuccessMsg:  fmt.Sprintf("Successfully created user: %s as Guest", in.Email),
		ErrorReason: "",
		TempUserId:  acc.ID,
	}, nil
}

func (ad *SysAccountRepo) genVerificationToken(param *coredb.QueryParams) (string, *dao.Account, error) {
	// TODO @gutterbacon: verification token, replace this with anything else
	// like OTP or anything
	vtoken, err := pass.GenHash(coredb.NewID())
	if err != nil {
		return "", nil, err
	}
	// TODO @gutterbacon: this is the part where we do email to user (verification) normally
	// update user's account table's verification_token
	acc, err := ad.store.GetAccount(param)
	if err != nil {
		return "", nil, err
	}
	acc.VerificationToken = vtoken
	err = ad.store.UpdateAccount(acc)
	if err != nil {
		return "", nil, err
	}
	return acc.VerificationToken, acc, nil
}

func (ad *SysAccountRepo) Login(ctx context.Context, in *pkg.LoginRequest) (*pkg.LoginResponse, error) {
	if in == nil {
		return &pkg.LoginResponse{}, status.Errorf(codes.Unauthenticated, "Can't authenticate: %v", sharedAuth.Error{Reason: sharedAuth.ErrInvalidParameters})
	}
	var claimant sharedAuth.Claimant

	u, err := ad.getAndVerifyAccount(ctx, in)
	if err != nil {
		return &pkg.LoginResponse{
			ErrorReason: err.Error(),
		}, err
	}
	claimant = u

	tokenPairs, err := ad.tokenCfg.NewTokenPairs(claimant)
	if err != nil {
		return &pkg.LoginResponse{
			ErrorReason: err.Error(),
		}, status.Errorf(codes.Unauthenticated, "Can't authenticate: %v", sharedAuth.Error{Reason: sharedAuth.ErrCreatingToken, Err: err})
	}

	req, err := ad.store.GetAccount(&coredb.QueryParams{Params: map[string]interface{}{"id": u.Id}})
	if err != nil {
		return nil, err
	}
	req.LastLogin = timestampNow()
	if err := ad.store.UpdateAccount(req); err != nil {
		return nil, err
	}
	errChan := make(chan error, 1)
	go func() {
		payloadBytes, err := coredb.MarshalToBytes(map[string]interface{}{"accessToken": tokenPairs.AccessToken, "refreshToken": tokenPairs.RefreshToken})
		if err != nil {
			ad.log.Debugf("error while marshal onLoginCreateInterceptor payload: %v", err)
			errChan <- err
			return
		}
		resp, err := ad.bus.Broadcast(ctx, &corepkg.EventRequest{
			EventName:   "onLoginCreateInterceptor",
			Initiator:   "sys-account",
			UserId:      u.Id,
			JsonPayload: payloadBytes,
		})
		if err != nil {
			ad.log.Debugf("error while calling onLoginCreateInterceptor: %v", err)
			errChan <- err
			return
		}
		ad.log.Debugf("event response: %v", string(resp.Reply))
	}()
	if err = <-errChan; err != nil {
		ad.log.Errorf("cannot call onLoginCreateInterceptor event: %v", err)
		// return nil, err
	}
	return &pkg.LoginResponse{
		Success:      true,
		AccessToken:  tokenPairs.AccessToken,
		RefreshToken: tokenPairs.RefreshToken,
		LastLogin:    req.LastLogin,
	}, nil
}

func (ad *SysAccountRepo) ForgotPassword(ctx context.Context, in *pkg.ForgotPasswordRequest) (*pkg.ForgotPasswordResponse, error) {
	if in == nil {
		return &pkg.ForgotPasswordResponse{}, status.Errorf(codes.InvalidArgument, "cannot request forgot password endpoint: %v", sharedAuth.Error{Reason: sharedAuth.ErrInvalidParameters})
	}
	// TODO @gutterbacon: this is where we should send an email to verify the user
	// We could also add this to audit log trail.
	// for now this method is a stub.
	vtoken, acc, err := ad.genVerificationToken(&coredb.QueryParams{Params: map[string]interface{}{"email": in.Email}})
	if err != nil {
		return &pkg.ForgotPasswordResponse{
			Success:                   false,
			SuccessMsg:                "",
			ErrorReason:               err.Error(),
			ForgotPasswordRequestedAt: timestampNow(),
		}, err
	}
	ad.log.Debugf("Generated Verification Token for ForgotPassword: %s", vtoken)
	errChan := make(chan error, 1)
	go func() {
		mailContent, err := ad.mailForgotPassword(acc.Email, vtoken)
		if err != nil {
			errChan <- err
			return
		}
		resp, err := ad.mail.SendMail(ctx, &corepkg.EmailRequest{
			Subject: fmt.Sprintf("Reset %s Password", acc.Email),
			Recipients: map[string]string{
				acc.Email: acc.Email,
			},
			Content: mailContent,
		})
		if err != nil {
			errChan <- err
			return
		}
		ad.log.Debugf("Sent Email to %s => %v\n", acc.Email, resp)
	}()
	if err = <-errChan; err != nil {
		ad.log.Errorf("Cannot send email: %v", err)
		// return nil, err
	}
	close(errChan)

	return &pkg.ForgotPasswordResponse{
		Success:                   true,
		SuccessMsg:                "Reset password token sent",
		ForgotPasswordRequestedAt: timestampNow(),
	}, nil
}

func (ad *SysAccountRepo) ResetPassword(ctx context.Context, in *pkg.ResetPasswordRequest) (*pkg.ResetPasswordResponse, error) {
	if in == nil {
		return &pkg.ResetPasswordResponse{}, status.Errorf(codes.InvalidArgument, "cannot request reset password endpoint: %v", sharedAuth.Error{Reason: sharedAuth.ErrInvalidParameters})
	}
	// TODO @gutterbacon: This is where we should send an email to verify the user
	// We could also add this to audit log trail.
	// but for now this method is a stub.
	if in.Password != in.PasswordConfirm {
		return nil, fmt.Errorf(sharedAuth.Error{Reason: sharedAuth.ErrVerifyPassword, Err: fmt.Errorf("password mismatch")}.Error())
	}
	acc, err := ad.store.GetAccount(&coredb.QueryParams{Params: map[string]interface{}{"email": in.Email}})
	if err != nil {
		ad.log.Debugf("error getting reset password account: %v", err)
		return &pkg.ResetPasswordResponse{
			Success:                  false,
			SuccessMsg:               "",
			ErrorReason:              err.Error(),
			ResetPasswordRequestedAt: timestampNow(),
		}, err
	}
	ad.log.Debugf("reset password account: %v", *acc)
	if acc.VerificationToken != in.VerifyToken {
		ad.log.Debugf("mismatch verification token: wanted %s\n got: %s", acc.VerificationToken, in.VerifyToken)
		return &pkg.ResetPasswordResponse{
			Success:                  false,
			SuccessMsg:               "",
			ErrorReason:              "verification token mismatch",
			ResetPasswordRequestedAt: timestampNow(),
		}, err
	}
	newPasswd, err := pass.GenHash(in.Password)
	if err != nil {
		return &pkg.ResetPasswordResponse{
			Success:                  false,
			SuccessMsg:               "",
			ErrorReason:              err.Error(),
			ResetPasswordRequestedAt: timestampNow(),
		}, err
	}
	acc.Password = newPasswd
	err = ad.store.UpdateAccount(acc)
	if err != nil {
		return &pkg.ResetPasswordResponse{
			Success:                  false,
			SuccessMsg:               "",
			ErrorReason:              err.Error(),
			ResetPasswordRequestedAt: timestampNow(),
		}, err
	}
	return &pkg.ResetPasswordResponse{
		Success:                  true,
		SuccessMsg:               "successfully reset password",
		ErrorReason:              "",
		ResetPasswordRequestedAt: timestampNow(),
	}, nil
}

func (ad *SysAccountRepo) VerifyAccount(ctx context.Context, in *pkg.VerifyAccountRequest) (*emptypb.Empty, error) {
	if in == nil {
		return nil, status.Errorf(codes.InvalidArgument, "cannot verify account: %v", sharedAuth.Error{Reason: sharedAuth.ErrInvalidParameters})
	}
	acc, err := ad.store.GetAccount(&coredb.QueryParams{Params: map[string]interface{}{"id": in.AccountId}})
	if err != nil {
		return nil, err
	}
	if acc.VerificationToken != in.VerifyToken {
		return nil, status.Errorf(codes.InvalidArgument, "cannot verify account: %v", sharedAuth.Error{Reason: sharedAuth.ErrVerificationTokenMismatch})
	}
	acc.Verified = true
	err = ad.store.UpdateAccount(acc)
	if err != nil {
		return nil, err
	}
	return &emptypb.Empty{}, nil
}

func (ad *SysAccountRepo) RefreshAccessToken(ctx context.Context, in *pkg.RefreshAccessTokenRequest) (*pkg.RefreshAccessTokenResponse, error) {
	if in == nil {
		return &pkg.RefreshAccessTokenResponse{
			ErrorReason: sharedAuth.Error{Reason: sharedAuth.ErrInvalidParameters}.Error(),
		}, status.Errorf(codes.InvalidArgument, "cannot request new access token: %v", sharedAuth.Error{Reason: sharedAuth.ErrInvalidParameters})
	}
	claims, err := ad.tokenCfg.ParseTokenStringToClaim(in.RefreshToken, false)
	if err != nil {
		return &pkg.RefreshAccessTokenResponse{
			ErrorReason: sharedAuth.Error{Reason: sharedAuth.ErrInvalidToken}.Error(),
		}, status.Errorf(codes.InvalidArgument, "refresh token is invalid: %v", sharedAuth.Error{Reason: sharedAuth.ErrInvalidToken})
	}
	newAccessToken, err := ad.tokenCfg.RenewAccessToken(&claims)
	if err != nil {
		return &pkg.RefreshAccessTokenResponse{
			ErrorReason: sharedAuth.Error{Reason: sharedAuth.ErrCreatingToken}.Error(),
		}, status.Errorf(codes.Internal, "cannot request new access token from claims: %v", err.Error())
	}
	return &pkg.RefreshAccessTokenResponse{
		AccessToken: newAccessToken,
	}, nil
}
