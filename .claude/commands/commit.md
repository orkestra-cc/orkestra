Steps to follow strictly:

## Pre-Commit Checks

1. Run `git status` to understand all changes
2. Review changes with `git diff` to understand impact

## Documentation Updates

5. For significant changes, update relevant documentation:
   - Module CLAUDE.md files if module behavior changed
   - Main CLAUDE.md if project-level changes

## Staging Strategy

6. Stage changes selectively:
   - `git add -A` for all changes

## Commit Message Generation

7. Generate conventional commit message following the pattern:

   ```
   <type>(<scope>): <subject>

   <body>

   <footer>
   ```

   Types:

   - feat: New feature
   - fix: Bug fix
   - docs: Documentation only
   - style: Code style (formatting, semicolons, etc)
   - refactor: Code refactoring
   - perf: Performance improvements
   - test: Adding tests
   - chore: Maintenance tasks
   - ci: CI/CD changes

   Scope: Module name (auth, scheduler, groups, etc.)

   Subject: Imperative mood, max 50 chars

   Body: Explain what and why, not how. Wrap at 72 chars.

   Footer: Breaking changes, issue references

## Final Steps

8. Display the commit for user review
9. Show files that were excluded
10. Remind about push command if on feature branch
