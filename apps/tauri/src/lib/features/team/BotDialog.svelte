<script lang="ts">
  import type {
    CreateBotRequest,
    ProfileDto,
    TeamAvatarDto,
    TeamHostDto,
    BotDto,
    SkillDto,
    McpServerDto,
    PluginDto,
  } from '@polymux/protocol';
  import {onMount, tick} from 'svelte';
  import {scrollFade} from '../../shared/scrollFade';
  import {deviceTypeIconName} from '../../shared/deviceTypeIcon';
  import Icon from '../../shared/components/Icon.svelte';
  import {activateModalDialog, trapModalFocus} from '../../shared/dialogFocus';
  import Menu from '../../shared/components/Menu.svelte';
  import {onThemeChange} from '../../shared/theme';
  import BloubAvatar from './BloubAvatar.svelte';
  import AvatarColorPicker from './AvatarColorPicker.svelte';
  import {
    adaptiveMonochromeAvatar,
    BLOUB_ADAPTIVE_MONOCHROME,
    bloubColorForTheme,
    isAdaptiveMonochromeAvatar,
    normalizeBloubAvatar,
    type BloubTheme,
  } from './bloub/colors';
  import {BLOUB_COLORS, BLOUB_SHAPES} from './bloub/model';

  const FIXED_BLOUB_COLORS = BLOUB_COLORS.slice(1, -1);
  const DARK_THEME_FIXED_COLORS = [...FIXED_BLOUB_COLORS].reverse();
  const isDarkTheme = (): boolean => typeof document !== 'undefined'
    && document.documentElement.dataset.theme === 'dark';

  export let bot: BotDto | null = null;
  export let profiles: ProfileDto[] = [];
  export let hosts: TeamHostDto[] = [];
  export let api: {
    skills?: { list(): Promise<SkillDto[]> };
    mcp?: { list(): Promise<McpServerDto[]> };
    plugins?: { list(): Promise<PluginDto[]> };
  } | null = null;
  export let availableConnections: {
    skills: SkillDto[];
    mcpServers: McpServerDto[];
    plugins: PluginDto[];
  } | null = null;
  export let busy = false;
  export let error = '';
  export let onSave: (request: CreateBotRequest) => void = () => {};
  export let onHostChange: (hostId: string) => void = () => {};
  export let onDelete: (() => void) | null = null;
  export let initialName = '';
  export let onClose: () => void = () => {};

  let dialog: HTMLDivElement;
  let nameInput: HTMLInputElement;
  let name = bot?.name ?? initialName;
  let role = bot?.role ?? '';
  let poolSkills: SkillDto[] = availableConnections?.skills ?? [];
  let poolMcpServers: McpServerDto[] = availableConnections?.mcpServers ?? [];
  let poolPlugins: PluginDto[] = availableConnections?.plugins ?? [];
  let loadingConnections = false;

  let selectedSkills = new Set<string>(bot?.skills ?? []);
  let selectedMcpServers = new Set<string>(bot?.mcpServers ?? []);
  let selectedPlugins = new Set<string>(bot?.plugins ?? []);

  $: if (availableConnections) {
    poolSkills = availableConnections.skills;
    poolMcpServers = availableConnections.mcpServers;
    poolPlugins = availableConnections.plugins;
  }
  $: hasAnyConnections = poolSkills.length > 0 || poolMcpServers.length > 0 || poolPlugins.length > 0;
  $: totalAssignedCount = selectedSkills.size + selectedMcpServers.size + selectedPlugins.size;
  let profileId = bot?.profileId ?? profiles[0]?.id ?? '';
  let agentKey = profileAgentKey(profiles.find((profile) => profile.id === profileId));
  let hostId = bot?.hostId ?? hosts.find((host) => host.isDefault)?.hostId ?? hosts[0]?.hostId ?? '';
  let darkTheme = isDarkTheme();
  let previewTheme: BloubTheme = darkTheme ? 'dark' : 'light';
  let previewFollowsApp = true;
  let avatar: TeamAvatarDto = normalizeBloubAvatar(
    bot?.avatar ?? adaptiveMonochromeAvatar({shape: 'circle'}),
  );
  let laptopAccess: 'off' | 'ask' = bot?.laptopAccess ?? 'ask';
  $: paletteColors = darkTheme ? DARK_THEME_FIXED_COLORS : FIXED_BLOUB_COLORS;
  $: eligibleProfiles = profiles.filter((profile) => profile.teamEligible !== false);
  $: agentOptions = [...new Map(eligibleProfiles.map((profile) => {
    const agent = profileAgent(profile);
    const value = profileAgentKey(profile);
    return [value, {
      value,
      label: agent.kind === 'acp' ? `${agent.name} · ACP` : agent.name,
    }];
  })).values()];
  $: if (eligibleProfiles.length && (!agentKey || !eligibleProfiles.some((profile) => profileAgentKey(profile) === agentKey))) {
    const preferred = eligibleProfiles.find((profile) => profile.id === bot?.profileId) ?? eligibleProfiles[0];
    agentKey = profileAgentKey(preferred);
  }
  $: agentProfiles = eligibleProfiles.filter((profile) => profileAgentKey(profile) === agentKey);
  $: if (agentProfiles.length && !agentProfiles.some((profile) => profile.id === profileId))
    profileId = agentProfiles[0].id;
  $: hostOptions = hosts.map((host) => ({
    value: host.hostId,
    label: `${host.deviceName}${host.mode === 'local' ? ' · This computer' : host.state === 'connected' ? '' : ' · Unreachable'}`,
    icon: deviceTypeIconName(host.deviceType),
    disabled: host.mode === 'remote' && host.state !== 'connected',
  }));
  const laptopAccessOptions = [
    {value: 'ask', label: 'Ask me when needed'},
    {value: 'off', label: 'Off'},
  ];

  $: selectedProfile = profiles.find((profile) => profile.id === profileId);
  $: valid = Boolean(name.trim() && role.trim() && hostId && profileId && selectedProfile?.teamEligible !== false && validAvatar(avatar));

  function profileAgent(profile?: ProfileDto): NonNullable<ProfileDto['agent']> {
    if (profile?.agent) return profile.agent;
    return profile?.source
      ? {kind: 'acp', id: profile.source.agentId, name: profile.source.agentName}
      : {kind: 'polymux', id: 'polymux', name: 'Polymux'};
  }

  function profileAgentKey(profile?: ProfileDto): string {
    if (!profile) return '';
    const agent = profileAgent(profile);
    return `${agent.kind}:${agent.id}`;
  }

  let connectionsOpen = Boolean(bot);

  onMount(() => {
    const restoreFocus = activateModalDialog(dialog);
    void tick().then(() => {
      if (!bot) nameInput?.focus();
    });
    const stopThemeChange = onThemeChange(() => {
      darkTheme = isDarkTheme();
      if (previewFollowsApp) previewTheme = darkTheme ? 'dark' : 'light';
    });

    if (!availableConnections && api) {
      loadingConnections = true;
      Promise.all([
        api.skills?.list?.() ?? Promise.resolve([]),
        api.mcp?.list?.() ?? Promise.resolve([]),
        api.plugins?.list?.() ?? Promise.resolve([]),
      ]).then(([s, m, p]) => {
        poolSkills = s;
        poolMcpServers = m;
        poolPlugins = p;
      }).catch(() => {}).finally(() => {
        loadingConnections = false;
      });
    }

    return () => {
      stopThemeChange();
      restoreFocus();
    };
  });

  function toggleSkill(skillName: string): void {
    const next = new Set(selectedSkills);
    if (next.has(skillName)) next.delete(skillName);
    else next.add(skillName);
    selectedSkills = next;
  }

  function toggleMcp(serverId: string): void {
    const next = new Set(selectedMcpServers);
    if (next.has(serverId)) next.delete(serverId);
    else next.add(serverId);
    selectedMcpServers = next;
  }

  function togglePlugin(pluginId: string): void {
    const next = new Set(selectedPlugins);
    if (next.has(pluginId)) next.delete(pluginId);
    else next.add(pluginId);
    selectedPlugins = next;
  }

  function updateAvatar(patch: Partial<TeamAvatarDto>): void {
    avatar = {...avatar, ...patch};
  }

  function selectFixedColor(color: string): void {
    avatar = {shape: avatar.shape, color};
  }

  function selectAdaptiveColor(): void {
    avatar = adaptiveMonochromeAvatar(avatar);
  }

  function setPreviewTheme(theme: BloubTheme): void {
    previewTheme = theme;
    previewFollowsApp = false;
  }

  function validAvatar(value: TeamAvatarDto): boolean {
    const validColor = (color: string): boolean => /^#[0-9a-f]{6}$/i.test(color);
    return validColor(value.color)
      && (!value.colorPair || validColor(value.colorPair.light) && validColor(value.colorPair.dark));
  }

  function save(): void {
    if (!valid || busy) return;
    onSave({
      name: name.trim(),
      role: role.trim(),
      profileId,
      hostId,
      avatar,
      laptopAccess,
      skills: [...selectedSkills],
      mcpServers: [...selectedMcpServers],
      plugins: [...selectedPlugins],
    });
  }

  function selectHost(nextHostId: string): void {
    if (nextHostId === hostId) return;
    hostId = nextHostId;
    agentKey = '';
    profileId = '';
    onHostChange(nextHostId);
  }

  function selectAgent(nextAgentKey: string): void {
    if (nextAgentKey === agentKey) return;
    agentKey = nextAgentKey;
    profileId = eligibleProfiles.find((profile) => profileAgentKey(profile) === nextAgentKey)?.id ?? '';
  }

  function keydown(event: KeyboardEvent): void {
    if (event.key === 'Escape') {
      event.preventDefault();
      onClose();
      return;
    }
    trapModalFocus(event, dialog);
    if (event.key === 'Enter' && (event.metaKey || event.ctrlKey)) save();
  }
</script>

<div class="team-dialog-backdrop" role="presentation" onclick={(event) => event.target === event.currentTarget && onClose()}>
  <div bind:this={dialog} class="team-dialog" role="dialog" aria-modal="true" aria-labelledby="team-dialog-title" tabindex="-1" onkeydown={keydown}>
    <header>
      <div>
        <h2 id="team-dialog-title">{bot ? `Edit ${bot.name}` : 'New bot'}</h2>
        <p>One bot, one private conversation.</p>
      </div>
      <button type="button" class="team-dialog-close" aria-label="Close" onclick={onClose}><Icon name="close" size={17}/></button>
    </header>

    <div class="team-dialog-body" use:scrollFade>
      <section class="team-identity-fields">
        <label>
          <span>Name</span>
          <input bind:this={nameInput} bind:value={name} autocomplete="off" placeholder="Maya"/>
        </label>
        <label>
          <span>Role</span>
          <input bind:value={role} autocomplete="off" placeholder="Research lead"/>
        </label>
        <div class="team-host-field">
          <span>Runs on</span>
          <div class="team-host-menu">
            <Menu
              options={hostOptions}
              value={hostId}
              label="Bot Host"
              wide
              floating
              onChange={selectHost}
            />
          </div>
          {#if bot && hostId !== bot.hostId}<small class="team-profile-note">Saving moves the bot, conversation and attachments to this Host.</small>{/if}
        </div>
        <div class="team-agent-field">
          <span>Agent</span>
          <div class="team-agent-menu">
            <Menu
              options={agentOptions}
              value={agentKey}
              label="Agent"
              wide
              floating
              onChange={selectAgent}
            />
          </div>
          {#if selectedProfile?.teamBlockedReason}<small class="team-profile-note">{selectedProfile.teamBlockedReason}</small>{/if}
        </div>
      </section>

      <section class="team-avatar-editor" aria-labelledby="team-avatar-heading">
        <div class="team-avatar-preview" class:dark={previewTheme === 'dark'}>
          <div class="team-avatar-preview-theme" role="radiogroup" aria-label="Avatar preview theme">
            {#each ['light', 'dark'] as theme (theme)}
              <button type="button" role="radio" aria-checked={previewTheme === theme} class:active={previewTheme === theme} onclick={() => setPreviewTheme(theme as BloubTheme)}>{theme === 'light' ? 'Light' : 'Dark'}</button>
            {/each}
          </div>
          <BloubAvatar {avatar} theme={previewTheme} expression="curious" size={82} label={`${name || 'Bot'} avatar`} paper="var(--team-avatar-preview-paper)"/>
          <div class="team-avatar-preview-copy"><strong>{name || 'Your bot'}</strong><span>{role || 'Choose a role'}</span></div>
        </div>

        <div class="team-avatar-setting">
          <span id="team-avatar-heading">Shape</span>
          <div class="team-avatar-choices shapes">
            {#each BLOUB_SHAPES as shape (shape.id)}
              <button type="button" class:selected={avatar.shape === shape.id} data-tooltip="none" aria-label={`${shape.label} shape`} aria-pressed={avatar.shape === shape.id} onclick={() => updateAvatar({shape: shape.id})}>
                <BloubAvatar avatar={{...avatar, shape: shape.id}} size={28} animated={false} paper="var(--app-surface)"/>
              </button>
            {/each}
          </div>
        </div>

        <div class="team-avatar-setting">
          <span>Colour</span>
          <div class="team-avatar-choices colors">
            <button
              type="button"
              class="adaptive"
              class:selected={isAdaptiveMonochromeAvatar(avatar)}
              aria-label="Ink in Light, Cream in Dark"
              aria-pressed={isAdaptiveMonochromeAvatar(avatar)}
              style:--avatar-light={BLOUB_ADAPTIVE_MONOCHROME.light}
              style:--avatar-dark={BLOUB_ADAPTIVE_MONOCHROME.dark}
              onclick={selectAdaptiveColor}
            ></button>
            {#each paletteColors as color (color.id)}
              <button type="button" class:selected={!avatar.colorPair && avatar.color.toLowerCase() === color.hex} aria-label={`${color.label} colour`} aria-pressed={!avatar.colorPair && avatar.color.toLowerCase() === color.hex} style:--avatar-color={color.hex} onclick={() => selectFixedColor(color.hex)}></button>
            {/each}
            <AvatarColorPicker value={bloubColorForTheme(avatar, previewTheme)} onChange={selectFixedColor}/>
          </div>
        </div>
      </section>

      <section class="team-behaviour">
        <div class="team-setting-row">
          <span><strong>Laptop access</strong><small>Never direct. Polymux asks before granting a short, auditable lease.</small></span>
          <div class="team-laptop-access-menu">
            <Menu
              options={laptopAccessOptions}
              value={laptopAccess}
              label="Laptop access"
              wide
              floating
              onChange={(access) => laptopAccess = access === 'off' ? 'off' : 'ask'}
            />
          </div>
        </div>
      </section>

      <section class="team-connections" aria-labelledby="team-connections-heading">
        <button type="button" class="team-connections-head" aria-expanded={connectionsOpen} onclick={() => connectionsOpen = !connectionsOpen}>
          <span>
            <strong id="team-connections-heading">Connections</strong>
            <small>Skills, MCP servers and plugins from the pool.</small>
          </span>
          <span class="team-connections-toggle">{#if hasAnyConnections}{totalAssignedCount} selected{:else}None{/if}<span class:open={connectionsOpen}><Icon name="chevron" size={14}/></span></span>
        </button>

        {#if connectionsOpen}
        {#if loadingConnections}
          <p class="team-connections-empty">Loading connections pool…</p>
        {:else if !hasAnyConnections}
          <p class="team-connections-empty">No connections in the workspace pool. Set up MCP servers, skills, or plugins in Connections.</p>
        {:else}
          {#if poolSkills.length}
            <div class="team-connections-category">
              <span>Skills</span>
              <div class="team-connections-chips" role="group" aria-label="Skills">
                {#each poolSkills as skill (skill.name)}
                  <button
                    type="button"
                    class="team-connection-chip"
                    class:selected={selectedSkills.has(skill.name)}
                    aria-pressed={selectedSkills.has(skill.name)}
                    onclick={() => toggleSkill(skill.name)}
                  >
                    <span class="team-connection-icon"><Icon name="sparkles" size={13}/></span>
                    <span>{skill.displayName || skill.name}</span>
                  </button>
                {/each}
              </div>
            </div>
          {/if}

          {#if poolMcpServers.length}
            <div class="team-connections-category">
              <span>MCP Servers</span>
              <div class="team-connections-chips" role="group" aria-label="MCP Servers">
                {#each poolMcpServers as mcp (mcp.id)}
                  <button
                    type="button"
                    class="team-connection-chip"
                    class:selected={selectedMcpServers.has(mcp.id)}
                    aria-pressed={selectedMcpServers.has(mcp.id)}
                    onclick={() => toggleMcp(mcp.id)}
                  >
                    <span class="team-connection-icon"><Icon name="mcp" size={13}/></span>
                    <span>{mcp.name || mcp.id}</span>
                  </button>
                {/each}
              </div>
            </div>
          {/if}

          {#if poolPlugins.length}
            <div class="team-connections-category">
              <span>Plugins</span>
              <div class="team-connections-chips" role="group" aria-label="Plugins">
                {#each poolPlugins as plugin (plugin.id)}
                  <button
                    type="button"
                    class="team-connection-chip"
                    class:selected={selectedPlugins.has(plugin.id)}
                    aria-pressed={selectedPlugins.has(plugin.id)}
                    onclick={() => togglePlugin(plugin.id)}
                  >
                    <span class="team-connection-icon"><Icon name="puzzle" size={13}/></span>
                    <span>{plugin.name}</span>
                  </button>
                {/each}
              </div>
            </div>
          {/if}
        {/if}
        {/if}
      </section>

      {#if error}<p class="team-dialog-error" role="alert">{error}</p>{/if}
    </div>

    <footer>
      {#if bot && onDelete}<button type="button" class="team-delete" disabled={busy} onclick={() => onDelete?.()}><Icon name="trash" size={14}/>Delete</button>{/if}
      <span></span>
      <button type="button" class="team-cancel" disabled={busy} onclick={onClose}>Cancel</button>
      <button type="button" class="team-save" disabled={!valid || busy} onclick={save}>{busy ? 'Saving…' : bot ? 'Save' : 'Add bot'}</button>
    </footer>
  </div>
</div>

<style>
  .team-dialog-body::-webkit-scrollbar{display:none}
  header>div{min-width:0;flex:1}h2{overflow:hidden;text-overflow:ellipsis;white-space:nowrap}.team-dialog-close{flex:none}
  .team-connections-head>span:first-child{min-width:0}
  .team-connection-chip{min-width:0;max-width:100%}.team-connection-chip>span:last-child{min-width:0;overflow:hidden;text-overflow:ellipsis;white-space:nowrap}
  @media(max-width:440px){.team-setting-row{align-items:stretch;flex-direction:column;gap:8px}.team-setting-row .team-laptop-access-menu{width:100%}}
  .team-delete { display:inline-flex; align-items:center; gap:8px; }
  .team-dialog-backdrop{position:fixed;z-index:180;inset:0;display:grid;place-items:center;padding:28px;background:rgba(13,16,24,.34);backdrop-filter:blur(3px)}
  .team-dialog{width:min(620px,calc(100vw - 40px));max-height:min(760px,calc(100vh - 44px));display:flex;flex-direction:column;overflow:hidden;border:1px solid var(--neutral-200);border-radius:18px;outline:none;background:var(--app-surface);color:var(--neutral-900);box-shadow:0 22px 70px rgba(0,0,0,.24);font-family:inherit;font-weight:450}
  header{display:flex;align-items:flex-start;justify-content:space-between;gap:20px;padding:20px 22px 14px;border-bottom:1px solid var(--neutral-150,var(--neutral-200))}h2{margin:0;color:var(--neutral-950);font-size:16px;font-weight:590;letter-spacing:-.015em;line-height:1.35}header p{margin:4px 0 0;color:var(--neutral-500);font-size:11.5px;font-weight:450;line-height:1.4}.team-dialog-close{width:28px;height:28px;display:grid;place-items:center;border:0;border-radius:8px;background:transparent;color:var(--neutral-500);cursor:pointer}.team-dialog-close:hover{color:var(--neutral-900)}
  .team-dialog-body{min-height:0;scrollbar-width:none;overflow-y:auto;padding:18px 22px}.team-identity-fields{display:grid;grid-template-columns:1fr 1fr;gap:13px}.team-host-field{grid-column:1/-1}.team-identity-fields label>span,.team-host-field>span,.team-agent-field>span,.team-avatar-setting>span{display:block;margin-bottom:6px;color:var(--neutral-600);font-size:11px;font-weight:600}.team-dialog input:not([type=checkbox]):not([type=color]){width:100%;height:36px;border:1px solid var(--neutral-250,var(--neutral-300));border-radius:9px;padding:0 10px;outline:none;background:var(--app-bg);color:var(--neutral-900);font:inherit;font-size:12px;font-weight:450}.team-dialog input:focus{border-color:var(--neutral-500)}.team-host-menu :global(.select-menu),.team-agent-menu :global(.select-menu){width:100%;min-width:0}.team-host-menu :global(.select-menu-trigger),.team-agent-menu :global(.select-menu-trigger){width:100%;height:36px;min-width:0;border-color:var(--neutral-250,var(--neutral-300));background:var(--app-bg);font:inherit;font-size:12px;font-weight:450}.team-profile-note{display:block;margin-top:5px;color:var(--neutral-500);font-size:10.5px;font-weight:450;line-height:1.4}
  .team-avatar-editor{display:grid;grid-template-columns:155px 1fr;gap:15px 18px;margin-top:18px;padding:15px;border:1px solid var(--neutral-150,var(--neutral-200));border-radius:13px;background:color-mix(in srgb,var(--app-bg) 72%,transparent)}.team-avatar-preview{--team-avatar-preview-paper:#fff;grid-row:1/3;display:flex;flex-direction:column;align-items:center;justify-content:center;min-height:168px;border-radius:10px;padding:9px;background:#fff;color:#202124;transition:background .14s ease,color .14s ease}.team-avatar-preview.dark{--team-avatar-preview-paper:#202020;background:#202020;color:#f1f1f1}.team-avatar-preview-theme{display:flex;gap:2px;margin-bottom:8px;padding:2px;border-radius:7px;background:#f0f0f0}.team-avatar-preview.dark .team-avatar-preview-theme{background:#303030}.team-avatar-preview-theme button{height:20px;border:0;border-radius:5px;padding:0 7px;background:transparent;color:#777;cursor:pointer;font:inherit;font-size:9px;font-weight:550}.team-avatar-preview-theme button:hover,.team-avatar-preview-theme button:focus-visible{outline:0;color:currentColor}.team-avatar-preview-theme button.active{background:#fff;color:#202124;box-shadow:0 1px 3px rgba(0,0,0,.1)}.team-avatar-preview.dark .team-avatar-preview-theme button{color:#999}.team-avatar-preview.dark .team-avatar-preview-theme button.active{background:#494949;color:#f1f1f1}.team-avatar-preview-copy{max-width:130px;margin-top:8px;text-align:center}.team-avatar-preview strong,.team-avatar-preview span{display:block;overflow:hidden;text-overflow:ellipsis;white-space:nowrap}.team-avatar-preview strong{font-size:12.5px;font-weight:590}.team-avatar-preview span{margin-top:2px;color:#8b8b8b;font-size:10.5px;font-weight:450}.team-avatar-preview.dark span{color:#999}
  .team-avatar-choices{display:flex;flex-wrap:wrap;gap:5px}.team-avatar-choices button{appearance:none;display:grid;place-items:center;border:1px solid transparent;padding:0;background:transparent;line-height:0;cursor:pointer}.team-avatar-choices.shapes button{width:34px;height:34px;border-radius:9px}.team-avatar-choices button.selected{border-color:var(--neutral-600);background:var(--neutral-100)}.team-avatar-choices.colors button{overflow:visible;flex:none;width:24px;height:24px;border:0;border-radius:50%;background:var(--avatar-color,transparent);background-origin:content-box;background-clip:content-box;box-shadow:0 0 0 1px rgba(0,0,0,.1)}.team-avatar-choices.colors button.adaptive{background-image:linear-gradient(to bottom right,var(--avatar-light) 0 49%,var(--avatar-dark) 51% 100%)}.team-avatar-choices.colors button.selected{padding:2px;box-shadow:0 0 0 2px var(--neutral-700);outline:0}.team-avatar-choices.colors button:focus-visible{outline:2px solid var(--focus-ring);outline-offset:3px}
  .team-behaviour{margin-top:16px;border:1px solid var(--neutral-150,var(--neutral-200));border-radius:12px;overflow:hidden}.team-setting-row{min-height:58px;display:flex;align-items:center;justify-content:space-between;gap:18px;padding:11px 13px}.team-setting-row>span{min-width:0;flex:1;margin:0}.team-setting-row strong,.team-setting-row small{display:block}.team-setting-row strong{color:var(--neutral-800);font-size:12.5px;font-weight:590}.team-setting-row small{margin-top:3px;color:var(--neutral-500);font-size:10.5px;font-weight:450;line-height:1.4}.team-laptop-access-menu{width:176px;flex:none}.team-laptop-access-menu :global(.select-menu){width:100%}.team-laptop-access-menu :global(.select-menu-trigger){height:34px;min-width:0;background:var(--app-bg);font-size:12px;font-weight:450}.team-dialog-error{margin:13px 0 0;color:var(--danger-600);font-size:11px;font-weight:450}
  .team-connections{margin-top:16px;border:1px solid var(--neutral-150,var(--neutral-200));border-radius:12px;overflow:hidden;padding:12px 14px}
  .team-connections-head{width:100%;display:flex;align-items:center;justify-content:space-between;gap:16px;border:0;padding:0;background:transparent;color:inherit;font:inherit;cursor:pointer;text-align:left}
  .team-connections-head strong{color:var(--neutral-800);font-size:12.5px;font-weight:590}
  .team-connections-head small{display:block;margin-top:3px;color:var(--neutral-500);font-size:10.5px;font-weight:450;line-height:1.4}
  .team-connections-toggle{flex:none;display:flex;align-items:center;gap:7px;font-size:11.5px;font-weight:550;color:var(--neutral-500)}
  .team-connections-toggle span{display:grid;place-items:center;transition:transform .15s ease}
  .team-connections-toggle span.open{transform:rotate(180deg)}
  .team-connections-category{margin-top:10px}
  .team-connections-category>span{display:block;margin-bottom:6px;color:var(--neutral-600);font-size:10.5px;font-weight:600;text-transform:uppercase;letter-spacing:.03em}
  .team-connections-chips{display:flex;flex-wrap:wrap;gap:6px}
  .team-connection-chip{display:inline-flex;align-items:center;gap:6px;height:28px;padding:0 10px;border:1px solid var(--neutral-200);border-radius:8px;background:var(--app-bg);color:var(--neutral-700);font:inherit;font-size:11.5px;font-weight:500;cursor:pointer;transition:border-color .15s ease,background-color .15s ease,color .15s ease}
  .team-connection-chip:hover{border-color:var(--neutral-400);color:var(--neutral-900)}
  .team-connection-chip.selected{background:var(--neutral-900);border-color:var(--neutral-900);color:var(--app-bg)}
  .team-connection-icon{display:grid;place-items:center;flex:none}
  .team-connections-empty{margin:6px 0 2px;color:var(--neutral-400);font-size:11px;line-height:1.4}
  footer{display:flex;align-items:center;gap:8px;padding:13px 22px 16px;border-top:1px solid var(--neutral-150,var(--neutral-200))}footer span{flex:1}footer button{height:34px;border:0;border-radius:9px;padding:0 13px;font:inherit;font-size:11.5px;font-weight:550;cursor:pointer}.team-cancel{background:var(--neutral-100);color:var(--neutral-700)}.team-save{background:var(--neutral-900);color:var(--app-bg)}.team-delete{background:transparent;color:var(--danger-600)}.team-dialog button:disabled{cursor:not-allowed;opacity:.45}
  @media(max-width:600px){.team-dialog-backdrop{padding:12px}.team-dialog{width:100%;max-height:calc(100vh - 24px)}.team-avatar-editor{grid-template-columns:1fr}.team-avatar-preview{grid-row:auto;min-height:150px}.team-identity-fields{grid-template-columns:1fr}.team-host-field{grid-column:auto}}
</style>
