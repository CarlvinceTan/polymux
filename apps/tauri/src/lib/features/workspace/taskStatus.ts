import {translate} from '../../../i18n';

export type TaskStatus = 'pending' | 'active' | 'completed' | 'failed';
export type TaskTone = 'running' | 'done' | 'failed';

/**
 * Collapses task states into the three the Summary indicator shows: still
 * working, finished, or something went wrong.
 */
export function taskStatusTone(status: TaskStatus): TaskTone {
  switch (status) {
    case 'completed':
      return 'done';
    case 'failed':
      return 'failed';
    default:
      return 'running';
  }
}

export function taskStatusLabel(status: TaskStatus): string {
  const tone = taskStatusTone(status);
  return translate(tone === 'done' ? 'task.completed' : tone === 'failed' ? 'task.failed' : 'task.working');
}
