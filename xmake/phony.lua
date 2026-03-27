-- xmake build DSL for phony targets
-- import("build", {inherit = true}) to inject into scope

import("core.project.depend")

-- Resolve glob patterns and literal paths into a flat file list.
-- All paths are relative to the project root.
--
--   sources("cli/**/*.go", "cli/go.mod", "web/src/**")
--
function sources(...)
    local root = os.projectdir()
    local result = {}
    for _, pattern in ipairs({...}) do
        local abs = path.join(root, pattern)
        local matched = os.files(abs)
        if #matched > 0 then
            for _, f in ipairs(matched) do
                table.insert(result, f)
            end
        elseif os.isfile(abs) then
            table.insert(result, abs)
        end
    end
    return result
end

-- Run build only when source files have changed.
--
--   on_changed(name, output, inputs, recipe)
--
--   name:   target name (used for cache file)
--   output: path relative to project root (used for mtime comparison)
--   inputs: file list from sources()
--   recipe: function to run when inputs are newer than output
--
function on_changed(name, output, inputs, recipe)
    local root    = os.projectdir()
    local outpath = path.join(root, output)
    depend.on_changed(recipe, {
        files      = inputs,
        lastmtime  = os.mtime(outpath),
        dependfile = path.join(root, ".xmake", name .. ".d"),
    })
end
