# Overlay HIL planner

An overlay is allowed when `hil_plan.py classify` returns `OVERLAY` and no
changed path requires a full image. The image-side contract is unchanged: any
change under `image/`, Buildroot configuration, image locks, boot media inputs,
profiles or platform boot code requires a full image. Unknown paths fail safe
to `FULL_IMAGE`.

Use the commit that produced the kit's base image as the diff base. The planner
compares that commit directly with the full PR head, so main-branch drift since
the image was made is included. Build from a clean worktree at the PR head.
HIL must run code identical to that PR head.

```sh
python3 scripts/hil_plan.py classify --repo . \
  --base-image-commit "$BASE_IMAGE_COMMIT" --head "$PR_HEAD"
# Stop and build a full image if the decision is FULL_IMAGE.
# From a clean worktree checked out at PR_HEAD, build the selected component
# binaries/core packages using their normal component build commands.
python3 scripts/hil_plan.py manifest --head "$PR_HEAD" --out /tmp/hil-manifest.json \
  /path/to/mister-agent=/usr/bin/mister-agent \
  /path/to/core.package=/media/fat/games/fes/core.package
# Deploy exactly the listed files to the kit, then run this command on the kit
# over SSH and save its output on Powerboat.
python3 scripts/hil_plan.py kit-command --manifest /tmp/hil-manifest.json
python3 scripts/hil_plan.py evidence --repo . \
  --base-image-commit "$BASE_IMAGE_COMMIT" --head "$PR_HEAD" \
  --base-image-sha256 "$BASE_LINUX_IMG_SHA256" \
  --manifest /tmp/hil-manifest.json --kit-sha256 /tmp/kit-sha256.txt \
  --out /tmp/hil-evidence.md
```

Evidence records the full head SHA, base image commit and `linux.img` hash,
classifier result and changed-path table. Every deployed file's SHA-256 is
computed on Powerboat and re-read on the kit after deployment; all must match.
Any `FULL_IMAGE` decision requires building a full image. `NO_DEPLOY_CHANGE`
does not call for deployment and can reuse prior HIL only when the tested code
is identical.
