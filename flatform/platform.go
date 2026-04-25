// flatform/platform.go
package platform

// Manager abstracts all OS-specific operations.
// Implementations: flatform/darwin, flatform/linux, flatform/windows
type Manager interface {
	// InjectBinDir prepends the platform's managed bin directory (and package
	// manager paths) into the *process* PATH at startup, so exec.LookPath
	// can find tools everywhere without modifying downloader/gallery/compressor.
	// macOS: adds /opt/homebrew/bin + /usr/local/bin
	// Linux: no-op (standard paths already in PATH)
	// Windows: prepends %APPDATA%\YTDown\bin
	InjectBinDir()

	// GetBinaryPath returns the absolute path to a tool, or "" if not found.
	// Checks PATH first, then platform-specific fallback paths (brew dirs, binDir…).
	GetBinaryPath(tool string) string

	// CheckDependencies returns the list of missing required tools.
	CheckDependencies() (missing []string, err error)

	// InstallDependency installs one tool using the OS-appropriate method:
	// macOS → brew install, Linux → apt/dnf/pacman + pip3, Windows → winget or .exe download
	InstallDependency(name string) error

	// InstallInstructions returns a user-facing dialog message listing what
	// will be installed and how.
	InstallInstructions(tools []string) string

	// PackageManagerName is the human-readable name ("Homebrew", "apt", "winget", …).
	PackageManagerName() string

	// PackageManagerAvailable returns true when the package manager is usable.
	PackageManagerAvailable() bool

	// UpgradeTool runs the best available self-upgrade for a tool.
	// macOS: yt-dlp -U first, then brew upgrade as fallback
	// Linux: pip3 --upgrade or package manager
	// Windows: winget upgrade or re-download .exe
	UpgradeTool(name, binaryPath string) error

	// GetLatestVersion returns the latest available version for a tool.
	// macOS: brew info, Linux/Windows: GitHub API
	GetLatestVersion(name string) string

	// LaunchSetup opens a terminal window that installs all dependencies.
	// macOS: osascript → Terminal.app
	// Linux: xterm / gnome-terminal
	// Windows: PowerShell / winget
	LaunchSetup() error

	// GetDownloadDir returns the default downloads folder for the user.
	GetDownloadDir() string

	// GetConfigDir returns the OS-appropriate config directory for YTDown.
	// macOS: ~/Library/Application Support/ytdown
	// Linux: ~/.config/ytdown
	// Windows: %APPDATA%\ytdown
	GetConfigDir() string

	// AppDataDir returns the base data directory (used for caches, bin/).
	AppDataDir() string

	// OpenFolder opens a directory in the native file manager.
	OpenFolder(path string) error

	// OpenFile opens a file with the system default application.
	// macOS: open, Linux: xdg-open, Windows: start / explorer
	OpenFile(path string) error

	// OSName returns a short OS name: "macOS", "Linux", or "Windows".
	OSName() string

	// UpdateAssetSuffix is the GitHub release asset suffix to download.
	// macOS → ".dmg"  |  Linux → ".AppImage"  |  Windows → "-Setup.exe"
	UpdateAssetSuffix() string

	// InstallAppUpdate downloads and replaces the running app with a new version.
	// parentPID is os.Getpid() — the updater waits for this process to exit first.
	InstallAppUpdate(downloadURL string, parentPID int) error
}
