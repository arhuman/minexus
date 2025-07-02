# Contributing to Minexus

Thank you for your interest in contributing to Minexus! This guide will help you get started with contributing to our distributed command execution system.

## Table of Contents

- [Code of Conduct](#code-of-conduct)
- [Getting Started](#getting-started)
- [Development Setup](#development-setup)
- [Making Changes](#making-changes)
- [Pull Request Process](#pull-request-process)
- [Code Quality Requirements](#code-quality-requirements)
- [Testing](#testing)
- [Coding Standards](#coding-standards)
- [Documentation](#documentation)
- [Reporting Issues](#reporting-issues)
- [Getting Help](#getting-help)

## Code of Conduct

This project adheres to the [Contributor Covenant Code of Conduct](CODE_OF_CONDUCT.md). By participating, you are expected to uphold this code. Please report unacceptable behavior to the project maintainers.

## Getting Started

### Prerequisites

- Go 1.23 or later
- Git
- Docker and Docker Compose (for integration tests)
- Protocol Buffers compiler (`protoc`)
- Make

### Development Setup

1. **Fork the repository** on GitHub
2. **Clone your fork** locally:
   ```bash
   git clone https://github.com/your-username/minexus.git
   cd minexus
   ```

3. **Add the upstream repository** as a remote:
   ```bash
   git remote add upstream https://github.com/original-owner/minexus.git
   ```

4. **Install development tools**:
   ```bash
   make tools
   ```
   This installs all necessary development and audit tools including:
   - `staticcheck` - Static analysis tool
   - `govulncheck` - Vulnerability scanner
   - `goimports` - Code formatting and import management
   - `golangci-lint` - Comprehensive Go linter
   - `revive` - Fast and extensible Go linter
   - Protocol buffer generators

5. **Generate your certs**

Generate your own certificates and place them into /internal/certs/file/
(Follow [the documentation about certificates generation](documentation/certificate_generation.md) for more details))

5. **Verify your setup**:
   ```bash
   make build
   make test
   ```

## Making Changes

### Branching Strategy

- Create a new branch for each feature or bug fix
- Use descriptive branch names (e.g., `feature/add-retry-mechanism`, `fix/connection-timeout`)
- Branch from `develop` unless otherwise specified

```bash
git checkout develop
git pull upstream develop
git checkout -b feature/your-feature-name
```

### Development Workflow

1. **Make your changes** in logical, atomic commits
2. **Write or update tests** for your changes
3. **Update documentation** as needed
4. **Run the full test suite** (`SLOW_TESTS=1 make test`) to ensure nothing is broken
5. **Run full audit (race conditions, security...)** (`make audit`) to really ensure nothing is broken
6. **Check that your code isn't overly complex (OPTIONAL)** (`go run github.com/fzipp/gocyclo/cmd/gocyclo@latest .`)
7. **Format and lint your code** using provided tools

## Pull Request Process

### Before Submitting a Pull Request

**IMPORTANT**: Before submitting any pull request, you must run the audit command and ensure all issues are resolved:

```bash
make audit
```

This command performs comprehensive code quality checks including:
- Module verification (`go mod verify`)
- Vet analysis (`go vet ./...`)
- Static analysis (`staticcheck`)
- Linting (`revive`)
- Vulnerability scanning (`govulncheck`)
- Race condition testing (`go test -race`)

**All issues reported by `make audit` must be fixed before your pull request will be considered.**

### Submission Steps

1. **Push your branch** to your fork:
   ```bash
   git push origin feature/your-feature-name
   ```

2. **Create a pull request** from your fork to the main repository

3. **Fill out the pull request template** completely, including:
   - Clear description of what the PR does
   - Link to any related issues
   - Testing instructions
   - Screenshots (if applicable)

4. **Ensure all CI checks pass**

5. **Respond to code review feedback** promptly

### Pull Request Guidelines

- **Title**: Use a clear, descriptive title
- **Description**: Provide a detailed description of your changes
- **Size**: Keep PRs focused and reasonably sized
- **Commits**: Use meaningful commit messages following conventional commit format
- **Tests**: Include tests for new functionality
- **Documentation**: Update relevant documentation

### Commit Message Format

We will try to follow the [Conventional Commits](https://www.conventionalcommits.org/) specification:

```
type(scope): description

[optional body]

[optional footer]
```

Types:
- `feat`: A new feature
- `fix`: A bug fix
- `docs`: Documentation only changes
- `style`: Changes that do not affect the meaning of the code
- `refactor`: A code change that neither fixes a bug nor adds a feature
- `perf`: A code change that improves performance
- `test`: Adding missing tests or correcting existing tests
- `chore`: Changes to the build process or auxiliary tools

Examples:
```
feat(minion): add connection retry mechanism
fix(nexus): resolve database connection timeout
docs(api): update gRPC service documentation
test(integration): add Docker Compose test scenarios
```

## Code Quality Requirements

### Automated Checks

All code must pass the following automated checks (run via `make audit`):

- **Go Vet**: Static analysis for common Go programming errors
- **Staticcheck**: Advanced static analysis for Go
- **Revive**: Fast, configurable, extensible, flexible, and beautiful linter for Go
- **Govulncheck**: Vulnerability scanning for Go dependencies
- **Race Detection**: Concurrent execution testing
- **Module Verification**: Dependency integrity checks

### Manual Review Criteria

- Code follows Go best practices and idioms
- Functions and methods are well-documented
- Error handling is appropriate and consistent
- Code is readable and maintainable
- Performance considerations are addressed
- Security implications are considered

## Testing

### Test Requirements

- **Unit tests** for all new functionality
- **Integration tests** for cross-component interactions
- **Minimum 80% code coverage** for new code
- **All existing tests must pass**

### Running Tests

```bash
# Run unit tests only
make test

# Run all tests including integration tests
SLOW_TESTS=1 make test

# Run tests with coverage analysis
make cover
```

### Test Guidelines

- Write tests that are fast, reliable, and maintainable
- Use table-driven tests where appropriate
- Mock external dependencies
- Test both success and failure scenarios
- Include edge cases and boundary conditions

## Coding Standards

### Go Style Guide

- Follow the [Go Code Review Comments](https://github.com/golang/go/wiki/CodeReviewComments)
- Use `gofmt` for formatting (automatically applied by `make tidy`)
- Use `goimports` for import management
- Follow standard Go naming conventions

### Project-Specific Guidelines

- Keep things as simple as possible, bu no not make them simpler
- Use structured logging with appropriate log levels
- Handle errors explicitly and provide meaningful error messages
- Use context for cancellation and timeouts
- Follow the established project architecture patterns
- Maintain backward compatibility where possible

## Documentation

### Documentation Requirements

- **Public APIs**: All exported functions, types, and methods must have Go doc comments
- **README updates**: Update README.md for user-facing changes
- **Architecture docs**: Update architecture documentation for significant changes
- **API docs**: Update gRPC/API documentation when protocols change

### Documentation Style

- Use clear, concise language
- Include examples where helpful
- Follow Go documentation conventions
- Keep documentation up-to-date with code changes

## Reporting Issues

### Before Reporting

- Check existing issues to avoid duplicates
- Search the documentation for solutions
- Try to reproduce the issue with the latest code

### Issue Template

When reporting issues, please include:

- **Environment**: Go version, OS, architecture
- **Steps to reproduce**: Detailed reproduction steps
- **Expected behavior**: What should happen
- **Actual behavior**: What actually happens
- **Logs**: Relevant log output or error messages
- **Additional context**: Any other relevant information

### Issue Labels

We use labels to categorize issues:
- `bug`: Something isn't working
- `enhancement`: New feature or request
- `documentation`: Improvements or additions to documentation
- `good first issue`: Good for newcomers
- `help wanted`: Extra attention is needed

## Getting Help

### Communication Channels

- **GitHub Issues**: For bug reports and feature requests
- **Pull Request Reviews**: For code-specific discussions

### Development Resources

- [Go Documentation](https://golang.org/doc/)
- [gRPC Go Documentation](https://grpc.io/docs/languages/go/)
- [Protocol Buffers Go Tutorial](https://developers.google.com/protocol-buffers/docs/gotutorial)
- [Docker Documentation](https://docs.docker.com/)

### Project Resources

- [`README.md`](README.md): Project overview and usage
- [`documentation/`](documentation/): Detailed documentation
- [`Makefile`](Makefile): Build and development commands
- [`.github/workflows/`](.github/workflows/): CI/CD configuration

## Recognition

Contributors who make significant contributions to the project will be recognized in the project's acknowledgments and may be invited to become maintainers.

Thank you for contributing to Minexus! Your contributions help make this project better for everyone.