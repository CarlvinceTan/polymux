<script module lang="ts">
  export type QueuedMessage = {
    id: string;
    text: string;
    files?: Array<{name: string; type: string}>;
  };
</script>

<script lang="ts">
  import Icon from '../../shared/components/Icon.svelte';
  import {t} from '../../../i18n';

  export let items: QueuedMessage[] = [];
  export let onSteer: (id: string) => void = () => {};
  export let onEdit: (id: string) => void = () => {};
  export let onDelete: (id: string) => void = () => {};
  export let onReorder: (sourceId: string, targetId: string) => void = () => {};

  let draggedId: string | null = null;
  let dropId: string | null = null;

  function startDrag(event: DragEvent, id: string): void {
    draggedId = id;
    event.dataTransfer?.setData('text/plain', id);
    if (event.dataTransfer) event.dataTransfer.effectAllowed = 'move';
  }

  function dragOver(event: DragEvent, id: string): void {
    event.preventDefault();
    dropId = id;
    if (event.dataTransfer) event.dataTransfer.dropEffect = 'move';
  }

  function drop(event: DragEvent, targetId: string): void {
    event.preventDefault();
    const sourceId = draggedId ?? event.dataTransfer?.getData('text/plain');
    if (sourceId) onReorder(sourceId, targetId);
    draggedId = null;
    dropId = null;
  }
</script>

<section class="stacked-bar queued-messages" aria-label={$t('queue.title')} data-visible-limit="4">
  <div class="queued-message-scroll" role="list">
    {#each items as item (item.id)}
      <!-- The row is the handle. A grip of its own bought nothing: it appeared
           on hover, which is when the row is already under the pointer, and it
           held a column open at rest that pushed the whole row in past the
           inset every other row keeps. Dragging from anywhere on the row is
           both the smaller target to explain and the larger one to hit. -->
      <div
        role="listitem"
        class:drop-target={dropId === item.id && draggedId !== item.id}
        class="stacked-row queued-message-row"
        draggable="true"
        aria-label={$t('queue.reorder', {text: item.text})}
        ondragstart={(event) => startDrag(event, item.id)}
        ondragend={() => { draggedId = null; dropId = null; }}
        ondragover={(event) => dragOver(event, item.id)}
        ondrop={(event) => drop(event, item.id)}
      >
        <span class="queue-state" aria-hidden="true"><Icon name="steer" size={15}/></span>
        {#if item.files?.length}
          <span class="queue-attachment" title={item.files[0].name}><Icon name={item.files[0].type.startsWith('image/') ? 'image' : 'file'} size={12}/></span>
        {/if}
        <span class="queue-copy">{item.text || $t('goal.reviewAttached')}</span>
        <div class="queue-actions">
          <button class="queue-steer" type="button" aria-label={$t('queue.steer')} data-tooltip-label={$t('queue.steer')} onclick={() => onSteer(item.id)}><Icon name="send" size={14}/></button>
          <button type="button" aria-label={$t('queue.edit')} data-tooltip-label={$t('common.edit')} onclick={() => onEdit(item.id)}><Icon name="edit" size={14}/></button>
          <button type="button" aria-label={$t('queue.delete')} data-tooltip-label={$t('common.delete')} onclick={() => onDelete(item.id)}><Icon name="trash" size={14}/></button>
        </div>
      </div>
    {/each}
  </div>
</section>
