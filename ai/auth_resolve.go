package ai

import (
	"errors"
	"fmt"
)

// ModelsErrorCode classifies a ModelsError (pi packages/ai/src/auth/resolve.ts).
type ModelsErrorCode string

const (
	ErrModelSource     ModelsErrorCode = "model_source"
	ErrModelValidation ModelsErrorCode = "model_validation"
	ErrProvider        ModelsErrorCode = "provider"
	ErrStream          ModelsErrorCode = "stream"
	ErrAuth            ModelsErrorCode = "auth"
	ErrOAuth           ModelsErrorCode = "oauth"
)

// ModelsError is a coded error from model/auth resolution. Cause is unwrappable.
type ModelsError struct {
	Code    ModelsErrorCode
	Message string
	Cause   error
}

func (e *ModelsError) Error() string {
	if e.Cause != nil {
		return fmt.Sprintf("%s: %s: %v", e.Code, e.Message, e.Cause)
	}
	return fmt.Sprintf("%s: %s", e.Code, e.Message)
}

func (e *ModelsError) Unwrap() error { return e.Cause }

// newModelsError builds a coded resolution error.
func newModelsError(code ModelsErrorCode, message string, cause error) *ModelsError {
	return &ModelsError{Code: code, Message: message, Cause: cause}
}

// AuthResolutionOverrides carry request-scoped apiKey/env into resolution so a
// provider's resolve() sees them (pi resolve.ts AuthResolutionOverrides,
// upstream ef231c49). An explicit apiKey resolves directly against a synthetic
// stored credential; env overlays the AuthContext and merges into a stored
// credential's env.
type AuthResolutionOverrides struct {
	APIKey string
	Env    map[string]string
}

// resolveProviderAuth is the auth resolution shared by the Models and
// ImagesModels collections (pi resolve.ts resolveProviderAuth). A stored
// credential owns the provider: ambient/env is consulted only when nothing is
// stored. No silent env fallback after a failed refresh or for a credential
// type without a matching handler. Returns (nil, nil) when unconfigured.
func resolveProviderAuth(
	providerID string,
	auth ProviderAuth,
	model *Model,
	credentials CredentialStore,
	ctx AuthContext,
	overrides *AuthResolutionOverrides,
) (*AuthResult, error) {
	// Request-scoped env is visible to the provider's resolve() (ef231c49).
	requestCtx := ctx
	if overrides != nil && len(overrides.Env) > 0 {
		requestCtx = overlayEnvAuthContext(ctx, overrides.Env)
	}

	// An explicit request apiKey resolves directly, ahead of any stored
	// credential (pi: overrides?.apiKey !== undefined).
	if overrides != nil && overrides.APIKey != "" && auth.APIKey != nil {
		return resolveApiKey(requestCtx, auth.APIKey, model, &Credential{
			Type: CredentialAPIKey,
			Key:  overrides.APIKey,
			Env:  overrides.Env,
		})
	}

	stored, err := readCredential(credentials, providerID)
	if err != nil {
		return nil, err
	}
	if stored != nil {
		if stored.Type == CredentialOAuth && auth.OAuth != nil {
			return resolveStoredOAuth(credentials, providerID, auth.OAuth, stored)
		}
		if stored.Type == CredentialAPIKey && auth.APIKey != nil {
			credential := stored
			if overrides != nil && len(overrides.Env) > 0 {
				clone := *stored
				clone.Env = mergeStringMap(stored.Env, overrides.Env) // overrides win
				credential = &clone
			}
			return resolveApiKey(requestCtx, auth.APIKey, model, credential)
		}
		return nil, nil
	}

	// Ambient (env vars, AWS profiles, ADC files).
	if auth.APIKey != nil {
		return resolveApiKey(requestCtx, auth.APIKey, model, nil)
	}
	return nil, nil
}

// overlayAuthContext makes overrides.env take precedence over the base
// AuthContext (pi: env[name] || base.env(name); empty falls through).
type overlayAuthContext struct {
	base AuthContext
	env  map[string]string
}

func (o overlayAuthContext) Env(name string) string {
	if v := o.env[name]; v != "" {
		return v
	}
	return o.base.Env(name)
}

func (o overlayAuthContext) FileExists(path string) bool { return o.base.FileExists(path) }

func overlayEnvAuthContext(base AuthContext, env map[string]string) AuthContext {
	return overlayAuthContext{base: base, env: env}
}

// resolveStoredOAuth resolves OAuth with double-checked locking: valid tokens
// cost zero locks; expired tokens lock, re-check expiry under the lock, refresh
// once globally, and persist the rotated credential before release.
func resolveStoredOAuth(
	credentials CredentialStore,
	providerID string,
	oauth *OAuthAuth,
	stored *Credential,
) (*AuthResult, error) {
	credential := stored.OAuthCredentials()

	if nowMillis() >= credential.Expires {
		// Optimistic check said expired; the authoritative check runs under the lock.
		post, err := credentials.Modify(providerID, func(current *Credential) (*Credential, error) {
			if current == nil || current.Type != CredentialOAuth {
				return nil, nil // logged out meanwhile
			}
			if nowMillis() < current.Expires {
				return nil, nil // another request refreshed
			}
			refreshed, rerr := oauth.Refresh(current.OAuthCredentials())
			if rerr != nil {
				return nil, newModelsError(ErrOAuth, "OAuth refresh failed for "+providerID, rerr)
			}
			return oauthCredential(refreshed), nil
		})
		if err != nil {
			var me *ModelsError
			if errors.As(err, &me) {
				return nil, err
			}
			return nil, newModelsError(ErrAuth, "Credential store modify failed for "+providerID, err)
		}
		if post == nil || post.Type != CredentialOAuth {
			return nil, nil // logged out meanwhile
		}
		credential = post.OAuthCredentials()
	}

	auth, err := oauth.ToAuth(credential)
	if err != nil {
		return nil, newModelsError(ErrOAuth, "OAuth auth derivation failed for "+providerID, err)
	}
	return &AuthResult{Auth: auth, Source: "OAuth"}, nil
}

// resolveApiKey runs a provider's api-key resolver and wraps failures.
func resolveApiKey(
	ctx AuthContext,
	apiKey *ApiKeyAuth,
	model *Model,
	credential *Credential,
) (*AuthResult, error) {
	res, err := apiKey.Resolve(model, ctx, credential)
	if err != nil {
		return nil, newModelsError(ErrAuth, "API key auth failed for provider "+string(model.Provider), err)
	}
	return res, nil
}

// readCredential reads from the store and wraps failures.
func readCredential(credentials CredentialStore, providerID string) (*Credential, error) {
	c, err := credentials.Read(providerID)
	if err != nil {
		return nil, newModelsError(ErrAuth, "Credential store read failed for "+providerID, err)
	}
	return c, nil
}
