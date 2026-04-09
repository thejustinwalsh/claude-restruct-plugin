---
name: disable-restruct
description: Disable restruct prompt refinement. Prompts pass through to Claude without local LLM preprocessing.
---

Run this command to disable restruct:

```bash
${CLAUDE_PLUGIN_ROOT}/bin/restruct disable
```

Confirm to the user that restruct is now disabled. Their prompts will pass through directly without refinement. To re-enable, they can run `/restruct:enable-restruct`.
