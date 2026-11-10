import type {main} from '../wailsjs/go/models';

type BoostSnapshot = Pick<main.BoostStatus,
  'suspendedApps' | 'savedMemoryKB' | 'trimmedProcessCount' | 'standbyPurged' |
  'timer05msActive' | 'mmcssActive' | 'powerPlanSwitched' | 'stoppedServices' |
  'extremeIActive' | 'extremeIIActive' | 'smartWakeActive'>;

export function summarizeBoost(status: BoostSnapshot) {
  const instances = (status.suspendedApps ?? []).reduce(
    (total, proc) => total + (proc.pids?.length || proc.instanceCount || 1), 0
  );
  const applied: string[] = [];
  if (status.trimmedProcessCount > 0) applied.push(`Gọt bộ nhớ làm việc (${status.trimmedProcessCount} tiến trình)`);
  if (status.standbyPurged) applied.push('Đã xả bộ nhớ đệm standby');
  if (status.timer05msActive) applied.push('Timer 0,5 ms');
  if (status.mmcssActive) applied.push('Ưu tiên MMCSS');
  if (status.powerPlanSwitched) applied.push('Đã chuyển gói nguồn');
  if (status.stoppedServices?.length) applied.push(`Tạm dừng ${status.stoppedServices.length} dịch vụ`);
  if (status.extremeIActive) applied.push('Extreme I');
  if (status.extremeIIActive) applied.push('Extreme II');
  if (status.smartWakeActive) applied.push('Smart Wake');
  return {instances, applied, workingSetKB: status.savedMemoryKB || 0};
}
