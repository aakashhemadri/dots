package installer

import (
	"io"
	"os"
	"path"
	"sync"

	"go.evanpurkhiser.com/dots/config"
)

const separator = string(os.PathSeparator)

// directoryMode is the mode used to create directories for installed dotfiles.
const directoryMode = 0755

// InstallConfig represents configuration options available for installing
// a single or set of dotfiles.
type InstallConfig struct {
	SourceConfig   *config.SourceConfig
	SourceLockfile *config.SourceLockfile

	// OverrideInstallPath specifies a path to install the dotfile at,
	// overriding the configuration in the SourceConfig.
	OverrideInstallPath string

	// ForceReinstall installs the dotfile even if the dotfile has not been
	// changed from its source. This implies that install scripts will be run.
	ForceReinstall bool

	// TODO We can probably add a channel here to pipe logging output so that we
	// can output some logging
}

// InstalledDotfile is a represents of the dotfile *after* it has been
// installed into the configuration directory.
type InstalledDotfile struct {
	*PreparedDotfile

	// InstallError represents an error that occurred during installation.
	InstallError error
}

// InstallDotfile is given a prepared dotfile and installation configuration
// and will perform all the necessary actions to install the file into it's
// target location.
func InstallDotfile(dotfile *PreparedDotfile, config InstallConfig) error {
	// Skip dotfiles that we failed to preapre
	if dotfile.PrepareError != nil {
		return nil
	}

	installPath := config.SourceConfig.InstallPath + separator + dotfile.Path

	if config.OverrideInstallPath != "" {
		installPath = config.OverrideInstallPath + separator + dotfile.Path
	}

	if !dotfile.IsChanged() && !config.ForceReinstall {
		return nil
	}

	// Removed
	if dotfile.Removed && !dotfile.RemovedNull {
		return os.Remove(installPath)
	}

	targetMode := dotfile.Permissions.New

	// Only filemode differs
	modeChanged := !dotfile.IsNew &&
		!dotfile.ContentsDiffer && dotfile.Permissions.IsChanged()

	if modeChanged {
		return os.Chmod(installPath, targetMode)
	}

	if err := os.MkdirAll(path.Dir(installPath), directoryMode); err != nil {
		return err
	}

	targetOpts := os.O_CREATE | os.O_TRUNC | os.O_WRONLY

	target, err := os.OpenFile(installPath, targetOpts, targetMode)
	if err != nil {
		return err
	}
	defer target.Close()

	source, err := OpenDotfile(dotfile.Dotfile, *config.SourceConfig)
	if err != nil {
		return err
	}
	defer source.Close()

	_, err = io.Copy(target, source)

	return err
}

// InstallDotfiles asynchronously calls InstalledDotfile on all passed
// PreparedDotfiles.
func InstallDotfiles(install PreparedInstall, config InstallConfig) []*InstalledDotfile {
	waitGroup := sync.WaitGroup{}
	waitGroup.Add(len(install.Dotfiles))

	installed := make([]*InstalledDotfile, len(install.Dotfiles))

	doInstall := func(i int, dotfile *PreparedDotfile) {
		err := InstallDotfile(dotfile, config)

		installed[i] = &InstalledDotfile{
			PreparedDotfile: dotfile,
			InstallError:    err,
		}

		waitGroup.Done()
	}

	for i, dotfile := range install.Dotfiles {
		go doInstall(i, dotfile)
	}

	waitGroup.Wait()

	return installed
}

// FinalizeInstall writes the updated lockfile after installation
func FinalizeInstall(installed []*InstalledDotfile, installConfig InstallConfig) {
	installedFiles := make([]string, 0, len(installed))

	for _, dotfile := range installed {
		if dotfile.Removed {
			continue
		}
		if dotfile.InstallError != nil {
			continue
		}

		installedFiles = append(installedFiles, dotfile.Path)
	}

	lockfile := installConfig.SourceLockfile
	lockfile.InstalledFiles = installedFiles
	config.WriteLockfile(lockfile, installConfig.SourceConfig)
}
