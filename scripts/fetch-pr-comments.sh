#!/bin/bash
# Fetch PR comments for the current branch and save them to pr_comments.txt

set -e

# 1. Get current branch
BRANCH=$(git branch --show-current)

if [ -z "$BRANCH" ]; then
  echo "Error: Could not determine current branch."
  exit 1
fi

# 2. Get repository name
# Try gh first
REPO=$(gh repo view --json nameWithOwner -q .nameWithOwner 2>/dev/null || true)

if [ -z "$REPO" ]; then
  # Fallback to git remote
  REPO=$(git remote -v | grep origin | grep fetch | head -n 1 | awk '{print $2}' | sed -E 's/.*github.com[:\/](.*)\.git/\1/')
fi

if [ -z "$REPO" ]; then
  echo "Error: Could not determine repository name."
  exit 1
fi

# 3. Get PR number for this branch
PR_NUMBER=$(gh pr view "$BRANCH" --json number -q .number 2>/dev/null || true)

if [ -z "$PR_NUMBER" ]; then
  echo "Error: No open pull request found for branch '$BRANCH' in $REPO."
  exit 1
fi

echo "Processing $REPO PR #$PR_NUMBER..."

# 4. Fetch comments and format them
# Using API to get code review comments (those with file paths)
gh api "repos/$REPO/pulls/$PR_NUMBER/comments" | jq -r '
  .[] | 
  "File: \(.path)\n" +
  "Line: \(.original_line // .line // "N/A")\n" +
  "Comment: \(.body)\n" +
  "-------------------"
' > pr_comments.txt

# Notify user
COUNT=$(grep -c -- "-------------------" pr_comments.txt || echo 0)
echo "Successfully fetched $COUNT comments and saved them to pr_comments.txt"
