package api

import (
	"net/http"

	"github.com/anand34577/docveta/internal/auth"
	"github.com/anand34577/docveta/internal/barcode"
	"github.com/anand34577/docveta/internal/identity"
	"github.com/anand34577/docveta/internal/notify"
	"github.com/anand34577/docveta/internal/platform/httpx"
)

type inviteCreated struct {
	Invite     *identity.Invite `json:"invite"`
	Link       string           `json:"link"` // shown once: only a hash is stored
	EmailSent  bool             `json:"email_sent"`
	EmailError string           `json:"email_error,omitempty"`
}

// registerAuth2 adds invitations and two-factor sign-in.
func (a *API) registerAuth2(mux router) {
	// --- Invitations (admin) -----------------------------------------------
	mux.HandleFunc("GET /api/v1/admin/invites", handle(func(r *http.Request, p *auth.Principal) (list[*identity.Invite], error) {
		i, err := a.Identity.ListInvites(r.Context(), p)
		return items(i), err
	}))
	mux.HandleFunc("POST /api/v1/admin/invites", handle(func(r *http.Request, p *auth.Principal) (*inviteCreated, error) {
		in, err := decode[struct {
			identity.NewInvite
			SendEmail bool `json:"send_email"`
		}](r)
		if err != nil {
			return nil, err
		}
		inv, token, err := a.Identity.CreateInvite(r.Context(), p, in.NewInvite)
		if err != nil {
			return nil, err
		}
		out := &inviteCreated{Invite: inv, Link: a.Cfg.BaseURL.String() + "/invite/" + token}
		if in.SendEmail && inv.Email != nil {
			msg := notify.Message{Title: p.Name + " invited you to Docveta",
				Body: "Docveta keeps your family's or team's documents organised and searchable. Use the link below to create your account. It works once and expires on " +
					inv.ExpiresAt.Format("2 January 2006") + ".",
				URL: out.Link}
			if err := a.Notify.SendEmailTo(r.Context(), *inv.Email, msg); err != nil {
				out.EmailError = err.Error()
			} else {
				out.EmailSent = true
			}
		}
		return out, nil
	}))
	mux.HandleFunc("DELETE /api/v1/admin/invites/{id}", handleNoContent(func(r *http.Request, p *auth.Principal) error {
		id, err := httpx.PathUUID(r, "id")
		if err != nil {
			return err
		}
		return a.Identity.RevokeInvite(r.Context(), p, id)
	}))

	// --- Invitations (the invitee, not signed in) ----------------------------
	mux.HandleFunc("GET /api/v1/invites/{token}", func(w http.ResponseWriter, r *http.Request) {
		pv, err := a.Identity.InvitePreviewFor(r.Context(), r.PathValue("token"))
		if err != nil {
			httpx.Error(w, r, err)
			return
		}
		httpx.JSON(w, http.StatusOK, pv)
	})
	mux.HandleFunc("POST /api/v1/invites/{token}/accept", func(w http.ResponseWriter, r *http.Request) {
		in, err := decode[identity.AcceptInvite](r)
		if err != nil {
			httpx.Error(w, r, err)
			return
		}
		token, u, err := a.Identity.AcceptInvitation(r.Context(), r.PathValue("token"), in, r.UserAgent())
		if err != nil {
			httpx.Error(w, r, err)
			return
		}
		a.setSessionCookie(w, token)
		httpx.JSON(w, http.StatusCreated, u)
	})

	// --- Two-factor sign-in ------------------------------------------------
	mux.HandleFunc("POST /api/v1/auth/login/2fa", func(w http.ResponseWriter, r *http.Request) {
		in, err := decode[struct {
			Challenge string `json:"challenge"`
			Code      string `json:"code"`
		}](r)
		if err != nil {
			httpx.Error(w, r, err)
			return
		}
		token, u, err := a.Identity.LoginTwoFactor(r.Context(), in.Challenge, in.Code, r.UserAgent())
		if err != nil {
			httpx.Error(w, r, err)
			return
		}
		a.setSessionCookie(w, token)
		httpx.JSON(w, http.StatusOK, u)
	})
	mux.HandleFunc("GET /api/v1/me/2fa", handle(func(r *http.Request, p *auth.Principal) (*identity.TwoFactorStatus, error) {
		return a.Identity.TwoFactorStatus(r.Context(), p.UserID)
	}))
	mux.HandleFunc("POST /api/v1/me/2fa/setup", handleAction(func(r *http.Request, p *auth.Principal) (map[string]string, error) {
		secret, uri, err := a.Identity.TwoFactorSetup(r.Context(), p)
		if err != nil {
			return nil, err
		}
		qr, _ := barcode.QRDataURL(uri, 220) // the secret is shown as text as well, so a failure here isn't fatal
		return map[string]string{"secret": secret, "uri": uri, "qr": qr}, nil
	}))
	mux.HandleFunc("POST /api/v1/me/2fa/enable", handleAction(func(r *http.Request, p *auth.Principal) (map[string][]string, error) {
		in, err := decode[struct {
			Code string `json:"code"`
		}](r)
		if err != nil {
			return nil, err
		}
		codes, err := a.Identity.TwoFactorEnable(r.Context(), p, in.Code)
		return map[string][]string{"recovery_codes": codes}, err
	}))
	mux.HandleFunc("POST /api/v1/me/2fa/disable", handleNoContent(func(r *http.Request, p *auth.Principal) error {
		in, err := decode[struct {
			Password string `json:"password"`
			Code     string `json:"code"`
		}](r)
		if err != nil {
			return err
		}
		return a.Identity.TwoFactorDisable(r.Context(), p, in.Password, in.Code)
	}))
	mux.HandleFunc("POST /api/v1/me/2fa/recovery-codes", handleAction(func(r *http.Request, p *auth.Principal) (map[string][]string, error) {
		in, err := decode[struct {
			Code string `json:"code"`
		}](r)
		if err != nil {
			return nil, err
		}
		codes, err := a.Identity.RegenerateRecoveryCodes(r.Context(), p, in.Code)
		return map[string][]string{"recovery_codes": codes}, err
	}))
	mux.HandleFunc("DELETE /api/v1/admin/users/{id}/2fa", handleNoContent(func(r *http.Request, p *auth.Principal) error {
		id, err := httpx.PathUUID(r, "id")
		if err != nil {
			return err
		}
		return a.Identity.AdminResetTwoFactor(r.Context(), p, id)
	}))
}
