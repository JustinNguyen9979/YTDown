// platform/platform.go
package platform

type Manager interface {
	CheckDependencies() (missing []string, err error)
	InstallDependency(name string) error
	GetDownloadDir() string
	GetConfigDir() string
	OpenFolder(path string) error
	GetBinaryPath(tool string) string
	OSName() string
}
