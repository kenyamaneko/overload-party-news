// Package rest は gateway 経由の公開 REST API の delivery 層。
// X-Internal-Auth (HMAC JWT) で認証し、player_id は sub クレーム経由で context に注入される。
package rest
