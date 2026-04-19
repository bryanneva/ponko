// CI pipeline for Ponko.
package main

import (
	"context"
	"dagger/ponko/internal/dagger"
)

const (
	goImage         = "golang:1.25"
	nodeImage       = "node:22"
	golangciImage   = "golangci/golangci-lint:v2.11.3"
	postgresImage   = "postgres:16"
	databaseURL     = "postgres://agent:agent@postgres:5432/agent_dev?sslmode=disable"
	repoWorkdir     = "/src"
	goModCachePath  = "/go/pkg/mod"
	goBuildCacheDir = "/root/.cache/go-build"
	npmCacheDir     = "/root/.npm"
)

type Ponko struct{}

// Run the full CI pipeline.
func (m *Ponko) Ci(
	ctx context.Context,
	source *dagger.Directory,
	// +optional
	anthropicApiKey *dagger.Secret,
) (string, error) {
	if _, err := m.Lint(ctx, source); err != nil {
		return "", err
	}
	if _, err := m.Build(ctx, source); err != nil {
		return "", err
	}
	if _, err := m.Test(ctx, source); err != nil {
		return "", err
	}
	if _, err := m.E2e(ctx, source, anthropicApiKey); err != nil {
		return "", err
	}
	return "ci passed", nil
}

// Run golangci-lint.
func (m *Ponko) Lint(ctx context.Context, source *dagger.Directory) (string, error) {
	sourceWithDist := m.sourceWithFrontend(source)
	return dag.Container().
		From(golangciImage).
		WithMountedDirectory(repoWorkdir, sourceWithDist).
		WithWorkdir(repoWorkdir).
		WithMountedCache(goModCachePath, dag.CacheVolume("go-mod")).
		WithMountedCache(goBuildCacheDir, dag.CacheVolume("go-build")).
		WithMountedCache("/root/.cache/golangci-lint", dag.CacheVolume("golangci-lint")).
		WithExec([]string{"golangci-lint", "run", "./..."}).
		Stdout(ctx)
}

// Build the slack bot, setup wizard, local CLI, and cloud runtime binaries.
func (m *Ponko) Build(ctx context.Context, source *dagger.Directory) (string, error) {
	return m.goContainer(m.sourceWithFrontend(source)).
		WithExec([]string{"go", "build", "-o", "/tmp/bin/slack", "./cmd/slack"}).
		WithExec([]string{"go", "build", "-o", "/tmp/bin/setup", "./cmd/setup"}).
		WithExec([]string{"go", "build", "-o", "/tmp/bin/cli", "./cmd/cli"}).
		WithExec([]string{"go", "build", "-o", "/tmp/bin/runtime", "./cmd/runtime"}).
		Stdout(ctx)
}

// Run unit tests with coverage.
func (m *Ponko) Test(ctx context.Context, source *dagger.Directory) (string, error) {
	return m.goContainer(m.sourceWithFrontend(source)).
		WithExec([]string{"go", "test", "-short", "-coverprofile=/tmp/coverage.out", "./..."}).
		WithExec([]string{"sh", "-c", "go tool cover -func=/tmp/coverage.out | tail -1"}).
		Stdout(ctx)
}

// Run end-to-end tests.
func (m *Ponko) E2e(
	ctx context.Context,
	source *dagger.Directory,
	// +optional
	anthropicApiKey *dagger.Secret,
) (string, error) {
	container := m.goContainer(m.sourceWithFrontend(source)).
		WithServiceBinding("postgres", m.postgres()).
		WithEnvVariable("DATABASE_URL", databaseURL)

	if anthropicApiKey != nil {
		container = container.WithSecretVariable("ANTHROPIC_API_KEY", anthropicApiKey)
	}

	return container.
		WithExec([]string{"go", "test", "-tags", "e2e", "./internal/e2e/..."}).
		Stdout(ctx)
}

func (m *Ponko) sourceWithFrontend(source *dagger.Directory) *dagger.Directory {
	webBuild := dag.Container().
		From(nodeImage).
		WithMountedDirectory(repoWorkdir, source).
		WithWorkdir(repoWorkdir+"/web").
		WithMountedCache(npmCacheDir, dag.CacheVolume("npm")).
		WithExec([]string{"npm", "ci"}).
		WithExec([]string{"npm", "run", "build"})

	return source.WithDirectory("web/dist", webBuild.Directory(repoWorkdir+"/web/dist"))
}

func (m *Ponko) goContainer(source *dagger.Directory) *dagger.Container {
	return dag.Container().
		From(goImage).
		WithMountedDirectory(repoWorkdir, source).
		WithWorkdir(repoWorkdir).
		WithMountedCache(goModCachePath, dag.CacheVolume("go-mod")).
		WithMountedCache(goBuildCacheDir, dag.CacheVolume("go-build"))
}

func (m *Ponko) postgres() *dagger.Service {
	return dag.Container().
		From(postgresImage).
		WithEnvVariable("POSTGRES_DB", "agent_dev").
		WithEnvVariable("POSTGRES_USER", "agent").
		WithEnvVariable("POSTGRES_PASSWORD", "agent").
		WithExposedPort(5432).
		AsService(dagger.ContainerAsServiceOpts{
			UseEntrypoint: true,
			Args: []string{
				"postgres",
				"-c",
				"listen_addresses=*",
			},
		})
}
