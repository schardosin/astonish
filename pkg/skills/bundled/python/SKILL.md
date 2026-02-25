---
name: python
description: "Python script execution, virtual environments, pip package management"
require_bins: ["python3"]
---

# Python

## When to Use
- Running Python scripts
- Managing virtual environments
- Installing Python packages
- Quick data processing or scripting tasks

## Virtual Environments
```
python3 -m venv .venv                   # Create virtual environment
source .venv/bin/activate               # Activate (bash/zsh)
deactivate                              # Deactivate
```

## Package Management (pip)
```
pip install <package>                   # Install package
pip install -r requirements.txt         # Install from requirements file
pip install -e .                        # Install current project in editable mode
pip freeze > requirements.txt           # Export installed packages
pip list                                # List installed packages
pip show <package>                      # Package info
pip uninstall <package>                 # Remove package
```

## Running Scripts
```
python3 script.py                       # Run a script
python3 -m module_name                  # Run a module
python3 -c "print('hello')"            # Run inline code
python3 -m http.server 8000            # Quick HTTP server
python3 -m json.tool < data.json       # Pretty-print JSON
```

## Common Patterns
```
# Quick data processing
python3 -c "import json, sys; data=json.load(sys.stdin); print(len(data))" < file.json

# Run pytest
python3 -m pytest tests/ -v

# Type checking
python3 -m mypy src/

# Formatting
python3 -m black src/
```

## Tips
- Always use virtual environments for project dependencies
- Prefer `python3` over `python` for clarity
- Use `python3 -m pip` instead of bare `pip` to ensure correct Python version
- Check for `pyproject.toml` or `setup.py` in the project root for build configuration
