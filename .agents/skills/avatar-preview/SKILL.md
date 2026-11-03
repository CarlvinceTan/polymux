---
name: avatar-preview
description: Create high-resolution GIF previews of Polymux avatars using the app’s actual renderer, animations, emotions and state effects. Use when showing or reviewing an avatar, checking avatar edits in motion, or comparing multiple avatars together in one GIF. Display only the resulting avatar GIF unless the user asks for explanation.
compatibility: Requires the Polymux checkout, macOS, Xcode and ffmpeg.
---

# Avatar preview

Create an animated preview, not an illustration, static contact sheet or mesh-only turntable. Use the current app code so the result includes its real face shapes, blinking, breathing, movements, effects and transitions.

Resolve `scripts/preview.sh` relative to this skill’s directory and invoke it by its absolute path. Keep the working directory at the repository root unless the user chose another output directory; output paths resolve from that working directory.

## Export

For one avatar:

```bash
bash scripts/preview.sh balloon --output avatar-preview.gif
```

For several avatars, including when reviewing edits to multiple avatars, give all their names to **one invocation** rather than exporting a separate GIF for each:

```bash
bash scripts/preview.sh balloon moon axolotl --output avatar-preview.gif
```

The exporter makes one looping GIF with a regular grid, equal-sized cells and synchronised playback. It defaults to **768 pixels per avatar at 25 fps**. It supports up to nine avatars; do not silently shrink their individual resolution to squeeze in more. Avatar labels and saved IDs are both accepted; `Balloon` resolves even though its persisted ID is `pinecone`.

Use `--list` to discover the current names and behaviours rather than guessing:

```bash
bash scripts/preview.sh --list
```

The complete default tour covers every current mood, animation state, expression and one-off movement (spin, bounce, nod). Timings come from the native definitions; long exports can take several minutes. Do not apply a short shell timeout. If the harness needs a background job, record its PID and log, wait for its exit, and cancel only by that PID when needed. Each mood gets a representative complete motion cycle, not every possible random variation of an indefinitely running idle animation. The JSON file beside the GIF records the exact coverage and timing without putting labels in the picture.

For a focused review only when the user asks for a subset:

```bash
bash scripts/preview.sh balloon --sections moods --moods thinking,happy,blocked,sleeping --output avatar-preview.gif
```

Other controls:

- `--size 1024` raises the resolution **per avatar**; range 256–2048.
- `--appearance dark` uses the app’s dark appearance; default is light. Export both only when requested or needed to review appearance-sensitive edits.
- `--color blue` selects the app palette for original geometric avatars; character colours still come from their actual design.
- `--sections moods,states,expressions,interactions` chooses coverage.
- `--seconds 2` overrides each segment duration for an explicitly shortened preview. This can cut animations short; do not describe it as a complete tour.
- `--plan` prints dimensions and the timeline without rendering.
- `--force` explicitly replaces an earlier GIF and its manifest. Use it only for this task’s own export or when replacement is requested.

## Fidelity and presentation

- The bundled exporter compiles the current `AvatarCanvas.swift`, `AvatarLayers.swift`, `AvatarView.swift`, palette and options sources against AvatarKit. It samples the same `AvatarMotion.frame` and draws the app’s `AvatarLayers` through Core Animation into a Metal texture, with a deterministic clock and personality seed.
- Keep the real rendering and natural animation speed. Do not substitute hand-drawn eyes, approximate motion, static mesh projections, new expressions or invented animation recipes.
- Show **only the avatar(s)** and their native effects on the app’s background. No app windows, editor controls, captions, titles, watermarks, legends or decorative borders.
- Do not launch the app, steal focus, change saved avatar choices, connect to a server, or touch personal app data to make an export. The helper runs headlessly; its build and temporary frames stay under `.build/avatar-preview/` and are cleaned up.
- Do not install missing dependencies without approval. If the native build or export fails, report the failure; never pass off an older GIF as the current result.

## Verify and deliver

1. Check the generated manifest against the requested avatars and coverage. Verify the GIF’s size, frame count, loop and frame timing with ImageIO, Pillow or ffprobe.
2. Inspect representative frames, including a moving state, an emotion, a turn and a state effect. In a grid, ensure every requested avatar is present, animates, fits its own cell and uses the same scale and timeline.
3. If editing the exporter, run its isolated regression checks:
   ```bash
   bash scripts/preview.sh --check
   ```
   These cover complete catalogue coverage, argument validation, image orientation, animated multi-row light/dark grids, looping, timing and overwrite protection.
4. Display the GIF inline, using the host’s image attachment support or a Markdown image link to the generated file. Link to the file if inline animated images are unsupported; do not open a viewer or browser unless asked.
5. The final response should be **only the GIF** when the user requested a preview. Add text only when asked, or when necessary to disclose incomplete coverage, a rendering issue or a failure.
