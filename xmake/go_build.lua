-- Go build helper for xmake phony targets
-- import("go_build", {inherit = true}) to inject go() into scope

-- Get version string from git tags
function _git_version()
    local v = try { function()
        return os.iorunv("git", {"describe", "--tags", "--always", "--dirty"})
    end } or "dev"
    return v:trim()
end

-- Build a Go binary. Paths are relative to project root.
--
--   go(outbin, opts)
--   go(outbin)
--
--   outbin: output path relative to project root
--   opts:   { workdir, tags, goos, goarch, ldflags }
--
function go(outbin, opts)
    opts = opts or {}
    local root    = os.projectdir()
    local abs_out = path.join(root, outbin)
    local version = _git_version()

    os.mkdir(path.directory(abs_out))

    local olddir     = os.cd(path.join(root, opts.workdir or "cli"))
    local old_goos   = os.getenv("GOOS")
    local old_goarch = os.getenv("GOARCH")

    if opts.goos   then os.setenv("GOOS",   opts.goos)   end
    if opts.goarch then os.setenv("GOARCH", opts.goarch) end

    local ldflags = opts.ldflags or ("-X github.com/tjw/restruct/cmd.Version=" .. version)
    local args = {"build"}
    if opts.tags then
        table.insert(args, "-tags")
        table.insert(args, opts.tags)
    end
    table.insert(args, "-ldflags")
    table.insert(args, ldflags)
    table.insert(args, "-o")
    table.insert(args, abs_out)
    table.insert(args, ".")

    os.execv("go", args)

    os.setenv("GOOS",   old_goos   or "")
    os.setenv("GOARCH", old_goarch or "")
    os.cd(olddir)
end
