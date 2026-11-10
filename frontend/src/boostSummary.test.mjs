import {test} from 'node:test';
import assert from 'node:assert/strict';
import {summarizeBoost} from './boostSummary.ts';

test('counts suspended process instances and reports applied options only', () => {
  const summary = summarizeBoost({active: true, suspendedApps: [{instanceCount: 3, pids: [1, 2, 3]}], savedMemoryKB: 204800, trimmedProcessCount: 2, standbyPurged: false, timer05msActive: true, mmcssActive: false, powerPlanSwitched: false, stoppedServices: ['WSearch'], extremeIActive: false, extremeIIActive: false, smartWakeActive: false});
  assert.equal(summary.instances, 3);
  assert.deepEqual(summary.applied, ['Gọt bộ nhớ làm việc (2 tiến trình)', 'Timer 0,5 ms', 'Tạm dừng 1 dịch vụ']);
  assert.equal(summary.workingSetKB, 204800);
});

test('handles absent arrays and zero results without inventing applied changes', () => {
  const summary = summarizeBoost({active: true, suspendedApps: null, stoppedServices: null, savedMemoryKB: 0});
  assert.equal(summary.instances, 0);
  assert.deepEqual(summary.applied, []);
  assert.equal(summary.workingSetKB, 0);
});
