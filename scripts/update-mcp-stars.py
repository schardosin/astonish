#!/usr/bin/env python3
"""
Update GitHub star counts for all MCP servers in store.json.

Usage:
    # With GitHub token (recommended - 5000 requests/hour):
    GITHUB_TOKEN=your_token python3 scripts/update-mcp-stars.py

    # Without token (limited to 60 requests/hour):
    python3 scripts/update-mcp-stars.py

Requirements:
    pip install requests
"""

import json
import os
import re
import sys
import time
from pathlib import Path

try:
    import requests
except ImportError:
    print("Error: 'requests' package not found. Install with: pip install requests")
    sys.exit(1)

STORE_FILE = Path(__file__).parent.parent / "pkg" / "mcpstore" / "data" / "store.json"
GITHUB_API = "https://api.github.com"


def extract_repo_info(github_url: str) -> tuple[str, str] | None:
    """Extract owner and repo from various GitHub URL formats."""
    if not github_url:
        return None
    
    # Match patterns like:
    # - https://github.com/owner/repo
    # - https://github.com/owner/repo/tree/main/path
    # - github.com/owner/repo
    patterns = [
        r"github\.com/([^/]+)/([^/]+?)(?:/tree/|/blob/|$|\.git)",
        r"github\.com/([^/]+)/([^/]+)$",
    ]
    
    for pattern in patterns:
        match = re.search(pattern, github_url)
        if match:
            owner = match.group(1)
            repo = match.group(2).rstrip("/")
            return owner, repo
    
    return None


def get_star_count(owner: str, repo: str, headers: dict) -> int | None:
    """Fetch star count for a GitHub repository."""
    url = f"{GITHUB_API}/repos/{owner}/{repo}"
    
    try:
        response = requests.get(url, headers=headers, timeout=10)
        
        if response.status_code == 200:
            data = response.json()
            return data.get("stargazers_count", 0)
        elif response.status_code == 404:
            print(f"  ⚠ Repository not found: {owner}/{repo}")
            return None
        elif response.status_code == 403:
            # Rate limited
            reset_time = response.headers.get("X-RateLimit-Reset")
            if reset_time:
                wait_seconds = int(reset_time) - int(time.time())
                print(f"  ⚠ Rate limited. Resets in {wait_seconds}s")
            return None
        else:
            print(f"  ⚠ Error {response.status_code} for {owner}/{repo}")
            return None
            
    except requests.RequestException as e:
        print(f"  ⚠ Request failed for {owner}/{repo}: {e}")
        return None


def check_rate_limit(headers: dict) -> tuple[int, int]:
    """Check current rate limit status."""
    url = f"{GITHUB_API}/rate_limit"
    try:
        response = requests.get(url, headers=headers, timeout=10)
        if response.status_code == 200:
            data = response.json()
            remaining = data["rate"]["remaining"]
            limit = data["rate"]["limit"]
            return remaining, limit
    except:
        pass
    return -1, -1


def main():
    # Check for GitHub token
    token = os.environ.get("GITHUB_TOKEN") or os.environ.get("GH_TOKEN")
    
    headers = {
        "Accept": "application/vnd.github.v3+json",
        "User-Agent": "astonish-mcp-store-updater"
    }
    
    if token:
        headers["Authorization"] = f"token {token}"
        print("✓ Using GitHub token (5000 requests/hour limit)")
    else:
        print("⚠ No GITHUB_TOKEN found - using unauthenticated access (60 requests/hour limit)")
        print("  Set GITHUB_TOKEN environment variable for higher limits")
    
    # Check rate limit
    remaining, limit = check_rate_limit(headers)
    if remaining >= 0:
        print(f"  Rate limit: {remaining}/{limit} remaining")
    
    # Load store.json
    if not STORE_FILE.exists():
        print(f"Error: Store file not found: {STORE_FILE}")
        sys.exit(1)
    
    with open(STORE_FILE, "r") as f:
        servers = json.load(f)
    
    print(f"\nUpdating star counts for {len(servers)} servers...")
    print("-" * 50)
    
    updated = 0
    failed = 0
    unchanged = 0
    
    for i, server in enumerate(servers):
        name = server.get("name", "Unknown")
        github_url = server.get("githubUrl", "")
        old_stars = server.get("githubStars", 0)
        
        repo_info = extract_repo_info(github_url)
        if not repo_info:
            print(f"[{i+1}/{len(servers)}] {name}: ⚠ Could not parse GitHub URL")
            failed += 1
            continue
        
        owner, repo = repo_info
        new_stars = get_star_count(owner, repo, headers)
        
        if new_stars is None:
            failed += 1
            continue
        
        if new_stars != old_stars:
            diff = new_stars - old_stars
            diff_str = f"+{diff}" if diff > 0 else str(diff)
            print(f"[{i+1}/{len(servers)}] {name}: {old_stars} → {new_stars} ({diff_str})")
            server["githubStars"] = new_stars
            updated += 1
        else:
            print(f"[{i+1}/{len(servers)}] {name}: {new_stars} (unchanged)")
            unchanged += 1
        
        # Small delay to be nice to the API
        time.sleep(0.1)
    
    print("-" * 50)
    print(f"Summary: {updated} updated, {unchanged} unchanged, {failed} failed")
    
    # Save updated store.json
    if updated > 0:
        with open(STORE_FILE, "w") as f:
            json.dump(servers, f, indent=2)
        print(f"\n✓ Saved updated star counts to {STORE_FILE}")
    else:
        print("\nNo changes to save.")


if __name__ == "__main__":
    main()
