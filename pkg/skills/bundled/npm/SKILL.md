---
name: npm
description: "Node.js package management — install, scripts, publish, workspaces"
require_bins: ["npm"]
---

# npm / Node.js

## When to Use
- Installing and managing Node.js dependencies
- Running project scripts (build, test, lint)
- Publishing packages
- Managing monorepo workspaces

## Common Commands

### Dependencies
```
npm install                             # Install all deps from package.json
npm install <package>                   # Add dependency
npm install -D <package>                # Add dev dependency
npm uninstall <package>                 # Remove dependency
npm update                              # Update all deps to latest allowed
npm outdated                            # Check for outdated packages
npm ls                                  # List installed packages
npm ls --depth=0                        # Top-level packages only
```

### Scripts
```
npm run <script>                        # Run a script from package.json
npm start                               # Run "start" script
npm test                                # Run "test" script
npm run build                           # Run "build" script
npx <command>                           # Run a package binary (temp install)
```

### Info
```
npm info <package>                      # Package metadata
npm search <keyword>                    # Search npm registry
npm view <package> versions             # List all published versions
```

### Publishing
```
npm login                               # Authenticate with npm registry
npm publish                             # Publish package
npm version patch|minor|major           # Bump version
npm pack                                # Create tarball without publishing
```

### Workspaces (Monorepo)
```
npm install -w packages/foo             # Install in a workspace
npm run build -w packages/foo           # Run script in workspace
npm run build --workspaces              # Run in all workspaces
```

## Tips
- Use `npx` to run one-off commands without global install: `npx create-react-app myapp`
- Use `npm ci` for clean installs in CI (respects lockfile exactly)
- Check `package.json` scripts section to see what commands are available
- Use `npm audit` to check for security vulnerabilities
