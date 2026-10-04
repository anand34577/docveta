# Single sign-on (OIDC)

People can sign in to Docveta with your organisation's identity provider: Authentik,
Keycloak, Authelia, Pocket ID, Zitadel, Kanidm, Microsoft Entra ID, Google, Auth0 and any
other OpenID Connect provider. It works in the web app and in the Android app.

## 1. Open Docveta at its real address

Single sign-on sends people back to Docveta at a fixed address, so first open Docveta the way
your users will: `https://docs.example.com`, or `http://192.168.1.20:8080` on a home network.
Not `localhost`, unless only this computer will use it.

If you set `DOCVETA_BASE_URL` ([Configuration](Configuration)), that address is used. While it
is left at the installer's `http://localhost:…`, Docveta uses the address your browser opened it
with, and remembers it for links in emails (Administration → System → Server address).

## 2. Create the application in your provider

Create an **OpenID Connect / OAuth2 application** ("confidential" client, *authorization code*
flow) and copy the **Redirect URI** shown in Docveta's **Administration → Authentication**.
It looks like:

```
https://docs.example.com/api/v1/auth/oidc/callback
```

Allowed scopes: `openid`, `profile`, `email` (and `groups` if you want group rules).

| Provider | Notes |
|---|---|
| Authentik | Applications → Create with provider → OAuth2/OpenID. Issuer: `https://auth.example.com/application/o/<slug>/` (keep the final `/`). Add the *groups* scope mapping for group rules. |
| Keycloak | Clients → Create client, *Client authentication* on. Issuer: `https://kc.example.com/realms/<realm>`. For groups add a *Group Membership* mapper with claim name `groups`, "full group path" off. |
| Authelia | `identity_providers.oidc.clients` with `redirect_uris` set to the address above and `scopes: [openid, profile, email, groups]`. Issuer: your Authelia address. |
| Pocket ID | OIDC Clients → Add. Issuer: your Pocket ID address. |
| Microsoft Entra ID | App registrations → New, platform *Web*, redirect URI above; create a client secret. Issuer: `https://login.microsoftonline.com/<tenant-id>/v2.0`. Group rules need the *groups* claim (Token configuration → Add groups claim). |
| Google | Google Cloud console → Credentials → OAuth client ID (Web application). Issuer: `https://accounts.google.com`. Google has no groups claim. |

## 3. Turn it on in Docveta

**Administration → Authentication → Single sign-on**

| Setting | Meaning |
|---|---|
| Issuer URL | from the table above. With or without the trailing `/` both work: Docveta tries both and keeps the one your provider publishes. |
| Client ID / secret | from your provider |
| Create accounts automatically | new people who sign in get an account and a personal space. Off: an administrator invites them first (Administration → Users → Invite). |
| Allowed groups | only members of these groups may sign in (empty = anyone your provider lets through) |
| Admin groups | members become Docveta administrators. (Removing someone from the group doesn't remove admin; do that in Users.) |
| Groups claim | the claim that lists groups, usually `groups` |
| Link to existing accounts by verified email | someone who already has a password account is linked automatically when the provider says their email is verified. Only enable it if you trust your provider to verify addresses. |
| Disable password sign-in | everyone must use single sign-on. Recovery: `docveta user reset-password` on the server. |

**Save** checks the issuer straight away and shows what went wrong if Docveta can't reach it.

## Connecting an existing account

People who already sign in with a password can add single sign-on themselves:
**Settings → Security → Single sign-on → Connect**. They sign in at the provider and come
back connected. **Disconnect** removes the link (not possible if it's their only way to sign in).

## The Android app

When single sign-on is on, the app shows a **Sign in with …** button. It opens the browser
for the provider's sign-in and returns to the app, which then keeps its own access token. The
link back to the app is protected with PKCE, so another app can't use it.

## Troubleshooting

| Message or symptom | Cause and fix |
|---|---|
| Provider shows *redirect_uri mismatch* / *invalid redirect URI* | The provider's redirect URI must be exactly the one shown in Administration → Authentication, opened at the address people use. Check `http` vs `https`, the port, and a missing or extra `/`. |
| "Could not load the OpenID configuration from …" | Docveta can't fetch `<issuer>/.well-known/openid-configuration`: wrong issuer URL, or the Docveta server can't reach the provider (DNS, firewall, a certificate it doesn't trust). |
| "The identity provider rejected Docveta's client ID or secret" | Re-enter the client secret; check the client is *confidential*. |
| "Your sign-in took too long, or cookies are blocked" | The sign-in took more than 10 minutes, or the browser blocked Docveta's cookie. Try again in a normal (not private) window. |
| "No Docveta account is connected to this sign-in" | Turn on *Create accounts automatically*, invite the person, or let them connect SSO from Settings → Security. |
| "Your account is not in a group that is allowed" | Add them to an allowed group, or check the *Groups claim* name and that the provider sends groups. |
| Behind a reverse proxy, the redirect URI says `http://` | Set `DOCVETA_BASE_URL=https://…`, or `DOCVETA_TRUSTED_PROXIES` to the proxy's address so Docveta believes its `X-Forwarded-Proto` header. |
