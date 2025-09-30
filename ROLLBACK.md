# Rollback Plan

## Current State
- **Working State**: All dynamic MIG implementation work preserved
- **Backup Branches**: 
  - `feat/dynamic-mig-implementation-backup`
  - `feat/add-fabric-manager-support-backup` 
  - `feat/test-coverage-foundation-backup`
- **Knowledge Artifacts**: `DESIGN.md`, `test-patterns/README.md`

## Rollback Procedures

### Quick Rollback (Restore Previous Working State)
```bash
# Restore to the last working commit
git checkout feat/dynamic-mig-implementation-backup
git checkout -b feat/dynamic-mig-implementation-restored

# Or restore to specific backup branch
git checkout feat/add-fabric-manager-support-backup
git checkout -b feat/add-fabric-manager-support-restored
```

### Partial Rollback (Cherry-pick Specific Features)
```bash
# Start from clean main
git checkout main
git checkout -b feat/selective-restore

# Cherry-pick only essential commits
git cherry-pick <commit-hash-for-mock-fixes>
git cherry-pick <commit-hash-for-device-state-validation>
git cherry-pick <commit-hash-for-mig-prepare-unprepare>
```

### Emergency Rollback (Reset to Main)
```bash
# Nuclear option - reset everything
git checkout main
git reset --hard origin/main
git clean -fd
```

## Recovery Steps

### 1. Verify Backup Branches
```bash
git branch -a | grep backup
git log --oneline feat/dynamic-mig-implementation-backup -5
```

### 2. Test Restored State
```bash
# After rollback, verify functionality
go test ./cmd/gpu-kubelet-plugin/... -v
go build ./cmd/gpu-kubelet-plugin/...
```

### 3. Restore Dependencies
```bash
# If vendor was modified
go mod vendor
go mod tidy
```

## Prevention Measures

### Before Major Changes
```bash
# Always create backup before major refactoring
git checkout -b backup-before-refactor
git add -A && git commit -m "Backup before refactor"
```

### Regular Checkpoints
```bash
# Create regular checkpoints
git tag checkpoint-$(date +%Y%m%d-%H%M%S)
```

### Documentation Updates
- Keep `DESIGN.md` updated with current decisions
- Update `test-patterns/README.md` with new patterns
- Document any breaking changes in `CHANGELOG.md`

## Recovery Contacts
- **Primary**: Development team lead
- **Secondary**: Architecture team
- **Emergency**: DevOps team

## Recovery Time Estimates
- **Quick Rollback**: 5-10 minutes
- **Partial Rollback**: 30-60 minutes  
- **Emergency Rollback**: 10-15 minutes
- **Full Recovery**: 2-4 hours
