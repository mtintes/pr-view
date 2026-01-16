# pr-view

Simple CLI to collect open pull requests from multiple GitHub repositories.

## Usage

- Add a repo (owner/repo):

```bash
pr-view add owner/repo
```
- Add a specific PR:

```bash
pr-view add "<PR_URL>"
```

- List open PRs across all configured repos:

```bash
pr-view list
```

## Install

```bash
brew install mtintes/pr-view/pr-view
```
or
```bash
brew tap mtintes/pr-view 
brew install pr-view
```

## Configuration

Repos are stored as JSON at `~/.configs/pr-view/repos.json`.

## Authentication

Set `GITHUB_TOKEN` environment variable for authenticated requests (higher rate limits):

```bash
export GITHUB_TOKEN=ghp_...
```

Build

```bash
make
```
