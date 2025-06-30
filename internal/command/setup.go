package command

// SetupCommands creates and registers all commands in the registry
func SetupCommands() *Registry {
	registry := NewRegistry()

	// Register system commands
	registry.Register(NewSystemInfoCommand())
	registry.Register(NewSystemOSCommand())

	// Register logging commands
	registry.Register(NewLoggingLevelCommand())
	registry.Register(NewLoggingIncreaseCommand())
	registry.Register(NewLoggingDecreaseCommand())

	// Register file commands (migrated to simplified system)
	registry.Register(NewFileGetCommand())
	registry.Register(NewFileCopyCommand())
	registry.Register(NewFileMoveCommand())
	registry.Register(NewFileInfoCommand())
	registry.Register(NewFileCommand()) // Unified file command for routing

	// Register shell commands (migrated to simplified system)
	registry.Register(NewShellCommand())  // Unified shell command
	registry.Register(NewSystemCommand()) // Backwards compatibility for system commands

	// Register docker-compose commands
	registry.Register(NewDockerComposePSCommand())
	registry.Register(NewDockerComposeUpCommand())
	registry.Register(NewDockerComposeDownCommand())
	registry.Register(NewDockerComposeCommand()) // Unified docker-compose command for routing

	return registry
}
