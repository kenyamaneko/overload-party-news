// Package apinews は news サービスが外部とやり取りする契約型を公開する。
//
// SSoT は data/openapi.yaml (REST) と data/asyncapi.yaml (Pub/Sub)。
// openapi_gen.go / asyncapi_gen.go は scripts/generate_types.sh で再生成される。
// 公開対象は gateway 向け公開 REST API の wire 型と news が購読する Pub/Sub イベントの型。
// ドメインモデルは internal/domain に置く。本パッケージは外部に公開する契約だけを束ねる。
package apinews
