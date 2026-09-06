# Guard action — milestone c

This directory is a scaffold. No action is executable yet.

Implement TypeScript/Node 24 main and post entrypoints using the Actions toolkit,
with a shared lifecycle used by the local coordinator. Main creates cleanup
context; post inventories, cleans, verifies and uploads untrusted preliminary
evidence. The trusted finalizer lives separately and never executes job code.

GitHub workflow definitions will remain disabled in the offline profile. Local
tests do not establish GitHub's own cancellation/post-step behavior.
