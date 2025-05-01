# Release Process

This document describes the process for releasing new versions of the Astonish package.

## Automated Release with GitHub Actions

Astonish uses GitHub Actions for automated building, testing, and publishing to PyPI.

### Release Steps

1. Update the version number in `pyproject.toml`
2. Commit the changes: `git commit -m "Bump version to x.y.z"`
3. Create a new tag: `git tag x.y.z`
4. Push the changes and tag: `git push && git push --tags`

When a new tag is pushed, GitHub Actions will automatically:
1. Build and test the package on multiple Python versions
2. Publish the package to PyPI if all tests pass

### GitHub Actions Workflows

- **Build and Test** (`.github/workflows/build.yml`): 
  - Runs on tag pushes (e.g., `1.0.0`)
  - Builds the package and runs tests on multiple Python versions (3.8, 3.9, 3.10, 3.11, 3.12)
  - Uploads build artifacts for inspection

- **Publish to PyPI** (`.github/workflows/publish.yml`):
  - Runs on tag pushes (e.g., `1.0.0`)
  - Builds and publishes the package to PyPI
  - Uses the PyPI API token stored in GitHub Secrets

## Manual Release (if needed)

If you need to release manually:

1. Update the version number in `pyproject.toml`
2. Build the package:
   ```
   make build
   ```
3. Upload to PyPI:
   ```
   python -m twine upload dist/*
   ```

## Required Secrets

For the automated publishing to work, you need to set up the following secret in your GitHub repository:

- `PYPI_API_TOKEN`: A PyPI API token with upload permissions
