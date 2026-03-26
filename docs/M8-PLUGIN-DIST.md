# M7: Plugin Distribution & Installation

## Goal

Package restruct as a Claude Code plugin that installs cleanly, works cross-platform, and provides a guided first-run experience.

## Depends On

M1 (Hook Protocol) — need correct hook configuration.
M6 (CLI UX) — need polished doctor/install commands for the setup flow.

---

## Tasks

### 7.1 — Plugin Manifest Finalization

**What:** Finalize `plugin/.claude-plugin/plugin.json` with all required fields.

**Current state:** Minimal metadata (name, version, keywords, license, author).

**Required additions:**
- `description` — one-line description for plugin discovery
- `homepage` — GitHub repo URL
- `repository` — GitHub repo URL
- `engines` — minimum Claude Code version compatibility
- `dependencies` — note Ollama as external dependency
- `postInstall` — command to run after plugin install (trigger setup wizard)
- Verify all fields against Claude Code's plugin spec (research current spec)

### 7.2 — Cross-Platform Binary Strategy

**What:** Ensure the plugin works on all platforms Claude Code supports.

**Current CI/CD:** Builds for darwin-arm64, darwin-amd64, linux-amd64, linux-arm64. Creates a launcher script that auto-detects platform.

**Improvements:**
- **Windows support:** Add windows-amd64 build target. The launcher script needs a `.bat` or `.ps1` equivalent.
- **Binary naming:** Use `restruct-{os}-{arch}` naming. The launcher/wrapper selects the right one.
- **Size optimization:** Build with `-ldflags="-s -w"` to strip debug info. Consider UPX compression if binaries exceed 20MB.
- **Verify Ollama availability per platform:** Ollama supports macOS, Linux, Windows. Document any platform-specific quirks.

### 7.3 — Installation Flow

**What:** A smooth installation experience from zero to working.

**Flow:**
```
1. User installs plugin (claude plugin install restruct)
2. postInstall triggers: restruct doctor --quiet
3. If doctor finds issues:
   a. Missing Ollama → prompt to install
   b. Missing model → prompt to pull (with progress)
   c. Hook not installed → auto-install
4. If all checks pass → "Restruct is ready! Your prompts will be automatically refined."
5. Offer to run a test refinement on a sample prompt
```

**Implementation:**
- New command: `restruct setup` — interactive setup wizard
- Combines doctor checks with guided remediation
- Non-interactive mode: `restruct setup --auto` (CI-friendly)
- The setup skill (`plugin/skills/setup/SKILL.md`) should invoke this

### 7.4 — Setup Skill Update

**What:** Update the Claude Code skill to provide a great in-Claude setup experience.

**Current:** `plugin/skills/setup/SKILL.md` and `plugin/skills/refine/SKILL.md` exist but may not align with the final implementation.

**Updates:**
- Setup skill: walks through `restruct setup`, explains what's happening
- Refine skill: allows manual refinement from within Claude (`/refine <prompt>`)
- Add skill: `plugin/skills/status/SKILL.md` — shows current restruct status (model, cache stats, last refinement)

### 7.5 — Uninstall & Cleanup

**What:** Clean uninstallation that removes all traces.

**`restruct uninstall`:**
- Remove hook from `.claude/settings.json`
- Remove hook from `~/.claude/settings.json` (if global)
- Optionally clear cache (`--purge` flag)
- Optionally remove config (`--purge` flag)
- Print confirmation of what was removed

### 7.6 — Plugin Release Automation

**What:** Automate the release pipeline.

**Current CI:** GitHub Actions builds on tag push, creates release with tarballs.

**Improvements:**
- **Changelog generation:** Auto-generate from conventional commit messages
- **Version bumping:** `make release-patch`, `make release-minor`, `make release-major`
- **Plugin packaging:** Build step creates a self-contained plugin tarball with:
  - Binaries for all platforms
  - Plugin manifest
  - Hook configs
  - Skills
  - Launcher script
- **Checksum file:** SHA256 checksums for all artifacts
- **GitHub Release:** Upload tarball + checksums + changelog

---

## Acceptance Criteria

- [ ] Plugin manifest is complete with all required fields
- [ ] Cross-platform binaries build and work (macOS, Linux, Windows)
- [ ] `restruct setup` provides guided installation from zero
- [ ] Skills updated to match final implementation
- [ ] `restruct uninstall` cleanly removes all traces
- [ ] Release automation produces versioned, checksummed plugin tarballs

## Files Modified

- `plugin/.claude-plugin/plugin.json` — complete manifest
- `plugin/hooks/hooks.json` — aligned with M1 findings
- `plugin/skills/setup/SKILL.md` — updated setup skill
- `plugin/skills/refine/SKILL.md` — updated refine skill
- New: `plugin/skills/status/SKILL.md` — status skill
- New: `cli/cmd/setup.go` — interactive setup wizard
- New: `cli/cmd/uninstall.go` — clean removal
- `.github/workflows/build.yml` — enhanced release automation
- `Makefile` — release targets

## Risk

**Medium.** Cross-platform testing is the main risk — Windows and Linux may have Ollama quirks we don't see on macOS. Mitigate by testing in CI with all platforms.
