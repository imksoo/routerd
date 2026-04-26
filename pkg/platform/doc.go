// Package platform centralizes OS-specific defaults and feature flags
// for routerd. Linux (Ubuntu/NixOS) is the primary target. FreeBSD
// support is in-progress: the platform package exposes the surface area
// renderers and reconciler need so additional backends can be added
// without sprinkling runtime.GOOS checks across the codebase.
package platform
