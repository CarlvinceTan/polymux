<script lang="ts">
  import {scrollFadeX} from '../../shared/scrollFade';

  let {role, raised = false}: {role: string; raised?: boolean} = $props();

  function fadeLabel(node: HTMLElement) {
    const fade = scrollFadeX(node);
    return () => fade.destroy();
  }
</script>

<span class="team-role-label-clip" {@attach fadeLabel}>
  <span class="team-role-label" class:raised>{role}</span>
</span>

<style>
  .team-role-label-clip{min-width:0;max-width:45%;display:flex;flex:0 1 auto;overflow:hidden}
  .team-role-label{width:max-content;display:inline-flex;flex:none;align-items:center;border:1px solid var(--team-role-outline);border-radius:6px;padding:0 6px;background:var(--team-role-surface);color:var(--team-role-text);font-size:11px;font-weight:500;line-height:17px;white-space:nowrap}
  .team-role-label.raised,:global(button:hover) .team-role-label,:global(button:focus-visible) .team-role-label{border-color:var(--team-role-outline-raised);background:var(--team-role-surface-raised);color:var(--team-role-text-raised)}
</style>
