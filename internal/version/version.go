package version

// Version is the CLI binary version. Set at build time via:
//
//	go build -ldflags="-X github.com/m2khosravi/kubefisher/internal/version.Version=v1.2.3"
var Version = "dev"
