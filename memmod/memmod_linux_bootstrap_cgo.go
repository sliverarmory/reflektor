//go:build linux && cgo && (386 || amd64 || arm64)

package memmod

import _ "github.com/sliverarmory/reflektor/memmod/internal/cgobootstrap"
