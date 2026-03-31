set_project("restruct")
set_version("0.1.0")

add_rules("mode.debug", "mode.release")
add_moduledirs("xmake")

------------------------------------------------------------------------
-- web: build the Vite/React dashboard
------------------------------------------------------------------------
target("web")
    set_kind("phony")
    set_default(false)

    on_build(function ()
        import("phony", {inherit = true})

        on_changed("web", "web/dist/index.html",
            sources("web/src/**", "web/package.json", "web/index.html",
                    "web/vite.config.ts", "web/tsconfig.json",
                    "web/tsconfig.app.json", "pnpm-lock.yaml"),
            function ()
                os.cd(path.join(os.projectdir(), "web"))
                os.execv("pnpm", {"install", "--frozen-lockfile"})
                os.execv("pnpm", {"run", "build"})
            end)
    end)

    on_clean(function ()
        os.tryrm(path.join(os.projectdir(), "web", "dist"))
    end)

------------------------------------------------------------------------
-- copy-web-dist: sync web/dist → cli/internal/server/dist for go:embed
------------------------------------------------------------------------
target("copy-web-dist")
    set_kind("phony")
    set_default(false)
    add_deps("web")

    on_build(function ()
        import("phony", {inherit = true})
        local root = os.projectdir()
        local src  = path.join(root, "web", "dist")
        local dst  = path.join(root, "cli", "internal", "server", "dist")

        if not os.isdir(src) then
            os.mkdir(dst)
            io.writefile(path.join(dst, "index.html"),
                "<html><body>Run 'pnpm build:web' first</body></html>")
            return
        end

        on_changed("copy-web-dist", "cli/internal/server/dist/index.html",
            sources("web/dist/**"),
            function ()
                os.tryrm(dst)
                os.cp(src, dst)
            end)
    end)

    on_clean(function ()
        os.tryrm(path.join(os.projectdir(), "cli", "internal", "server", "dist"))
    end)

-- Map xmake platform names to uname/Go names
function _platform_suffix()
    local host = os.host()
    if host == "macosx" then host = "darwin" end
    return host .. "-" .. os.arch()
end

------------------------------------------------------------------------
-- cli: build the Go binary for the host platform
------------------------------------------------------------------------
target("cli")
    set_kind("binary")
    add_deps("copy-web-dist")
    set_targetdir(path.join(os.projectdir(), "plugin", "bin"))
    set_basename("restruct-" .. _platform_suffix())

    on_build(function (target)
        import("phony", {inherit = true})
        import("go_build", {inherit = true})

        local suffix = os.host() == "macosx" and "darwin" or os.host()
        suffix = suffix .. "-" .. os.arch()
        on_changed("cli", "plugin/bin/restruct-" .. suffix,
            sources("cli/**/*.go",
                    "cli/internal/db/migrations/*.sql",
                    "cli/internal/server/dist/**",
                    "cli/internal/prompt/system_prompt.tmpl",
                    "cli/go.mod", "cli/go.sum"),
            function ()
                go("plugin/bin/restruct-" .. suffix, {
                    tags = is_mode("debug") and "debug" or nil,
                })
            end)
    end)

    on_clean(function ()
        local host = os.host() == "macosx" and "darwin" or os.host()
        os.tryrm(path.join(os.projectdir(), "plugin", "bin", "restruct-" .. host .. "-" .. os.arch()))
        os.cd(path.join(os.projectdir(), "cli"))
        os.execv("go", {"clean"})
    end)

------------------------------------------------------------------------
-- cli-*: release builds for each platform
------------------------------------------------------------------------
for _, plat in ipairs({
    {goos = "darwin", goarch = "arm64", suffix = "darwin-arm64"},
    {goos = "darwin", goarch = "amd64", suffix = "darwin-x86_64"},
    {goos = "linux",  goarch = "amd64", suffix = "linux-x86_64"},
}) do
    local _goos, _goarch, _suffix = plat.goos, plat.goarch, plat.suffix

    target("cli-" .. _suffix)
        set_kind("phony")
        set_default(false)
        set_group("release")
        add_deps("copy-web-dist")

        on_build(function ()
            import("go_build", {inherit = true})
            go("plugin/bin/restruct-" .. _suffix, {
                goos   = _goos,
                goarch = _goarch,
                tags   = is_mode("debug") and "debug" or nil,
            })
        end)

        on_clean(function ()
            os.tryrm(path.join(os.projectdir(), "plugin", "bin", "restruct-" .. _suffix))
        end)
    target_end()
end
