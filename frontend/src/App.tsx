import {useEffect, useMemo, useState} from 'react';
import {
  ArrowsClockwise,
  CheckCircle,
  Cpu,
  Eye,
  Fire,
  Gauge,
  Lightning,
  MagnifyingGlass,
  PauseCircle,
  PlayCircle,
  ShieldCheck,
  Sliders,
  SquaresFour,
  Timer,
  WarningCircle,
} from '@phosphor-icons/react';
import {
  ExportUnknownProcessesReport,
  GetBoostStatus,
  GetUnknownProcessesReport,
  ListRunningProcesses,
  ResumeProcess,
  StartBoost,
  StopBoost,
} from '../wailsjs/go/main/App';
import {main} from '../wailsjs/go/models';
import {summarizeBoost} from './boostSummary';
import './App.css';

type Page = 'overview' | 'settings';
type OverviewTab = 'options' | 'apps';
type FilterMode = 'all' | 'recommended' | 'selected' | 'unknown';

const WINDOWS_SERVICES = [
  {
    id: 'WSearch',
    name: 'Windows Search (WSearch)',
    desc: 'Có thể tạm dừng lập chỉ mục tìm kiếm trong phiên Boost.',
  },
  {
    id: 'SysMain',
    name: 'SysMain (Superfetch)',
    desc: 'Có thể tạm dừng SysMain nếu dịch vụ đang chạy.',
  },
  {
    id: 'DiagTrack',
    name: 'Connected User Experiences (DiagTrack)',
    desc: 'Có thể tạm dừng dịch vụ chẩn đoán nền nếu đang chạy.',
  },
  {
    id: 'Spooler',
    name: 'Print Spooler',
    desc: 'Có thể tạm dừng dịch vụ máy in nếu bạn không cần in.',
  },
];

function formatRAM(kb: number): string {
  if (!kb || kb <= 0) return '0 MB';
  if (kb >= 1024 * 1024) {
    return `${(kb / 1024 / 1024).toFixed(1)} GB`;
  }
  return `${Math.max(1, Math.round(kb / 1024))} MB`;
}

function App() {
  const [page, setPage] = useState<Page>('overview');
  const [overviewTab, setOverviewTab] = useState<OverviewTab>('options');
  const [processes, setProcesses] = useState<main.RunningProcess[]>([]);
  const [selectedPIDs, setSelectedPIDs] = useState<Set<number>>(new Set());
  const [loadingProcs, setLoadingProcs] = useState(false);
  const [busyBoost, setBusyBoost] = useState(false);
  const [errorMsg, setErrorMsg] = useState('');
  const [search, setSearch] = useState('');
  const [filterMode, setFilterMode] = useState<FilterMode>('all');
  const [unknownReport, setUnknownReport] = useState<main.UnknownProcessReport | null>(null);
  const [savedNotice, setSavedNotice] = useState('');
  const [lastRestore, setLastRestore] = useState<{
    instances: number; applied: string[]
  } | null>(null);

  // Main Boost Options
  const [pauseApps, setPauseApps] = useState(true);
  const [smartWake, setSmartWake] = useState(true);
  const [trimWorkingSet, setTrimWorkingSet] = useState(true);
  const [purgeStandby, setPurgeStandby] = useState(false);
  const [timerResolution05ms, setTimerResolution05ms] = useState(true);
  const [mmcssGpuPriority, setMmcssGpuPriority] = useState(true);
  const [reduceWindowsTasks, setReduceWindowsTasks] = useState(false);
  const [highPerformance, setHighPerformance] = useState(true);
  const [extremeModeI, setExtremeModeI] = useState(false);
  const [extremeModeII, setExtremeModeII] = useState(false);
  const [selectedServices, setSelectedServices] = useState<string[]>([
    'WSearch',
    'SysMain',
    'DiagTrack',
  ]);

  const [boostStatus, setBoostStatus] = useState<main.BoostStatus>(
    main.BoostStatus.createFrom({
      active: false,
      startedAt: '',
      suspendedApps: [],
      savedMemoryKB: 0,
      stoppedServices: [],
      powerPlanSwitched: false,
      originalPowerScheme: '',
      warnings: [],
    })
  );

  const loadSystemState = async (preserveSelection = true) => {
    setLoadingProcs(true);
    setErrorMsg('');
    try {
      const [procList, status] = await Promise.all([
        ListRunningProcesses(),
        GetBoostStatus(),
      ]);
      setProcesses(procList || []);
      setBoostStatus(status);

      setSelectedPIDs((prev) => {
        const validPIDs = new Set((procList || []).map((p) => p.pid));
        return new Set([...prev].filter((pid) => validPIDs.has(pid)));
      });
    } catch {
      setErrorMsg('Không thể đọc danh sách tiến trình từ Windows Task Manager.');
    } finally {
      setLoadingProcs(false);
    }
  };

  useEffect(() => {
    void loadSystemState(false);
  }, []);

  const selectedList = useMemo(
    () => processes.filter((p) => selectedPIDs.has(p.pid)),
    [processes, selectedPIDs]
  );

  const estimatedMemoryKB = useMemo(
    () => selectedList.reduce((sum, item) => sum + item.memoryKB, 0),
    [selectedList]
  );

  const filteredProcesses = useMemo(() => {
    const q = search.trim().toLowerCase();
    return processes.filter((proc) => {
      if (filterMode === 'recommended' && !proc.recommended) return false;
      if (filterMode === 'selected' && !selectedPIDs.has(proc.pid)) return false;
      if (filterMode === 'unknown' && proc.knownInKB) return false;
      if (!q) return true;
      return (
        proc.name.toLowerCase().includes(q) ||
        (proc.friendlyName || '').toLowerCase().includes(q) ||
        (proc.publisher || '').toLowerCase().includes(q) ||
        (proc.description || '').toLowerCase().includes(q) ||
        proc.category.toLowerCase().includes(q) ||
        String(proc.pid).includes(q)
      );
    });
  }, [processes, filterMode, search, selectedPIDs]);

  const unknownCount = useMemo(
    () => processes.filter((p) => !p.knownInKB).length,
    [processes]
  );

  const openUnknownReportModal = async () => {
    setSavedNotice('');
    try {
      const report = await GetUnknownProcessesReport();
      setUnknownReport(report);
    } catch {
      setErrorMsg('Không thể tạo bản xem trước danh sách tiến trình lạ.');
    }
  };

  const handleExportUnknownReport = async () => {
    try {
      const report = await ExportUnknownProcessesReport();
      setUnknownReport(report);
      if (navigator.clipboard && report.prettyJSON) {
        await navigator.clipboard.writeText(report.prettyJSON).catch(() => {});
      }
      setSavedNotice(`Đã lưu mẫu ẩn danh & sao chép JSON vào Clipboard: ${report.savedPath}`);
    } catch {
      setErrorMsg('Không thể xuất gói đóng góp tiến trình lạ.');
    }
  };

  const togglePID = (pid: number) => {
    setSelectedPIDs((prev) => {
      const next = new Set(prev);
      if (next.has(pid)) {
        next.delete(pid);
      } else {
        next.add(pid);
      }
      return next;
    });
  };

  const selectRecommended = () => {
    const next = new Set<number>();
    for (const proc of processes) {
      if (proc.recommended && !proc.caution) {
        next.add(proc.pid);
      }
    }
    setSelectedPIDs(next);
  };

  const clearSelection = () => {
    setSelectedPIDs(new Set());
  };

  const handleStartBoost = async () => {
    setBusyBoost(true);
    setErrorMsg('');
    try {
      const req = main.BoostRequest.createFrom({
        pauseApps,
        selectedPIDs: Array.from(selectedPIDs),
        smartWake,
        trimWorkingSet,
        purgeStandby,
        timerResolution05ms,
        mmcssGpuPriority,
        reduceWindowsTasks,
        serviceNames: selectedServices,
        highPerformance,
        extremeModeI,
        extremeModeII,
      });
      const status = await StartBoost(req);
      setBoostStatus(status);
      if (status.active) setLastRestore(null);
      await loadSystemState(true);
      if (status.warnings?.length) setErrorMsg(status.warnings.join(' · '));
    } catch (error) {
      const detail = error instanceof Error ? error.message : String(error);
      const status = await GetBoostStatus().catch(() => null);
      if (status) setBoostStatus(status);
      await loadSystemState(true);
      setErrorMsg(`Không thể kích hoạt Game Boost: ${detail}`);
    } finally {
      setBusyBoost(false);
    }
  };

  const handleStopBoost = async () => {
    setBusyBoost(true);
    setErrorMsg('');
    setLastRestore(null);
    const before = summarizeBoost(boostStatus);
    try {
      const status = await StopBoost();
      setBoostStatus(status);
      if (!status.active) {
        setLastRestore({instances: before.instances, applied: before.applied});
      }
      if (status.warnings?.length) {
        setErrorMsg(status.warnings.join(' · '));
      }
      await loadSystemState(true);
    } catch {
      const status = await GetBoostStatus().catch(() => null);
      if (status) setBoostStatus(status);
      setErrorMsg('Khôi phục chưa hoàn tất. Kiểm tra cảnh báo bên dưới và thử lại.');
    } finally {
      setBusyBoost(false);
    }
  };

  const handleResumeSingle = async (pid: number) => {
    try {
      const status = await ResumeProcess(pid);
      setBoostStatus(status);
    } catch {
      setErrorMsg(`Không thể khôi phục tiến trình PID ${pid}.`);
    }
  };

  const toggleService = (id: string) => {
    setSelectedServices((prev) =>
      prev.includes(id) ? prev.filter((item) => item !== id) : [...prev, id]
    );
  };

  const restoreDefaultSettings = () => {
    setPauseApps(true);
    setTrimWorkingSet(true);
    setPurgeStandby(false);
    setTimerResolution05ms(false);
    setMmcssGpuPriority(false);
    setReduceWindowsTasks(false);
    setHighPerformance(false);
    setSelectedServices(['WSearch', 'SysMain', 'DiagTrack']);
    selectRecommended();
  };

  const isBoosting = boostStatus.active;
  const boostSummary = summarizeBoost(boostStatus);

  return (
    <div className={`app-shell ${isBoosting ? 'is-active' : ''}`}>
      <aside className="sidebar">
        <div className="brand-header">
          <div className="brand-mark">
            <Lightning size={18} weight="fill" />
          </div>
          <div className="brand-copy">
            <strong>Owlys Pulse</strong>
            <span>System Booster</span>
          </div>
        </div>

        <nav className="sidebar-nav">
          <button
            type="button"
            className={`nav-btn ${page === 'overview' ? 'active' : ''}`}
            onClick={() => setPage('overview')}
          >
            <span className="nav-btn-left">
              <Gauge size={18} weight={page === 'overview' ? 'fill' : 'regular'} />
              <span>Tổng quan</span>
            </span>
          </button>

          <button
            type="button"
            className={`nav-btn ${page === 'settings' ? 'active' : ''}`}
            onClick={() => setPage('settings')}
          >
            <span className="nav-btn-left">
              <Sliders size={18} weight={page === 'settings' ? 'fill' : 'regular'} />
              <span>Thiết lập</span>
            </span>
          </button>
        </nav>

        <div className="sidebar-footer">
          <div className="status-pill">
            <span className="status-dot" />
            <span>{isBoosting ? 'Đang Boost' : 'Sẵn sàng Boost'}</span>
          </div>
          <p>
            {isBoosting
              ? 'Nhấn Dừng Boost để khôi phục. Nếu có lỗi, xem cảnh báo và thử lại.'
              : 'Không cần chọn game. Bật Boost bất cứ lúc nào trước hoặc trong khi chơi.'}
          </p>
        </div>
      </aside>

      <main className="workspace">
        {errorMsg && (
          <div className="notice-banner">
            <WarningCircle size={18} weight="fill" />
            <span>{errorMsg}</span>
          </div>
        )}

        {page === 'overview' && (
          <>
            <header className="page-header">
              <div className="page-header-copy">
                <h1>Tăng tốc hệ thống</h1>
                <p>Ngủ đông ứng dụng nền, dọn sạch bộ nhớ và ưu tiên tài nguyên tối đa cho trận game của bạn.</p>
              </div>
              <button
                type="button"
                className="cta-btn secondary"
                disabled={loadingProcs}
                onClick={() => void loadSystemState(true)}
              >
                <ArrowsClockwise size={15} />
                <span>{loadingProcs ? 'Đang quét...' : 'Làm mới tiến trình'}</span>
              </button>
            </header>

            <section className="hero-card">
              <div className="hero-top">
                <div className="hero-main">
                  <div className="hero-icon">
                    <Lightning size={24} weight="fill" />
                  </div>
                  <div className="hero-copy">
                    <h2>
                      {isBoosting
                        ? 'Chế độ Game Boost đang hoạt động'
                        : 'Sẵn sàng tối ưu hóa cho phiên chơi game'}
                    </h2>
                    <p>
                      {isBoosting
                        ? `Đã đưa ${boostSummary.instances} tiến trình nền vào ngủ đông an toàn.`
                        : 'Chỉ đóng băng tạm thời ứng dụng bạn chọn, không tắt ngang hay làm mất dữ liệu đang mở.'}
                    </p>
                  </div>
                </div>

                {isBoosting ? (
                  <button
                    type="button"
                    className="cta-btn stop"
                    disabled={busyBoost}
                    onClick={() => void handleStopBoost()}
                  >
                    <PlayCircle size={18} weight="fill" />
                    <span>{busyBoost ? 'Đang khôi phục...' : 'Dừng Boost & Khôi phục'}</span>
                  </button>
                ) : (
                  <button
                    type="button"
                    className="cta-btn primary"
                    disabled={busyBoost || loadingProcs}
                    onClick={() => void handleStartBoost()}
                  >
                    <Lightning size={18} weight="fill" />
                    <span>{busyBoost ? 'Đang kích hoạt...' : 'Bật Game Boost ngay'}</span>
                  </button>
                )}
              </div>

              <div className="hero-metrics">
                <div className="metric-item">
                  <span>Tiến trình tạm dừng</span>
                  <strong>
                    {isBoosting
                      ? `${boostSummary.instances} tiến trình`
                      : pauseApps
                        ? `${selectedPIDs.size} nhóm đã chọn`
                        : 'Đang tắt'}
                  </strong>
                </div>
                <div className="metric-item">
                  <span>RAM giải phóng tạm thời</span>
                  <strong>
                    {isBoosting
                      ? formatRAM(boostSummary.workingSetKB)
                      : pauseApps
                        ? `~${formatRAM(estimatedMemoryKB)}`
                        : '0 MB'}
                  </strong>
                  <small>{isBoosting ? 'Đã thu hồi Working Set' : 'Ước tính theo app đã chọn'}</small>
                </div>
                <div className="metric-item">
                  <span>Thay đổi đã áp dụng</span>
                  <strong>
                    {isBoosting
                      ? boostSummary.applied.length ? boostSummary.applied.join(' · ') : 'Đóng băng ứng dụng'
                      : 'Xem các tùy chọn bên dưới'}
                  </strong>
                </div>
              </div>
            </section>

            {isBoosting && (
              <section className="boost-report" role="status">
                <strong>Đã kích hoạt Game Boost!</strong>
                <p>
                  Đã đưa {boostSummary.instances} tiến trình nền vào ngủ đông, giải phóng tạm thời khoảng {formatRAM(boostSummary.workingSetKB)} RAM cho game. Phím tắt khôi phục: <code>Ctrl + Alt + Shift + R</code> hoặc <code>Ctrl + Shift + F12</code>. Chúc bạn chơi game mượt mà!
                </p>
              </section>
            )}

            {(isBoosting && (boostStatus.warnings?.length ?? 0) > 0) && (
              <section className="boost-report warning" role="alert">
                <strong>Cảnh báo phiên Boost</strong>
                <ul>{boostStatus.warnings.map((warning, index) => <li key={index}>{warning}</li>)}</ul>
              </section>
            )}
            {!isBoosting && lastRestore && (
              <section className="boost-report" role="status">
                <strong>Đã khôi phục toàn bộ hệ thống!</strong>
                <p>
                  Phiên chơi đã kết thúc. Pulse đã rã đông thành công {lastRestore.instances} tiến trình và trả lại 100% trạng thái Windows (Nguồn điện, Services, MMCSS). Hệ thống sẵn sàng cho công việc bình thường!
                </p>
              </section>
            )}
            {!isBoosting && (boostStatus.warnings?.length ?? 0) > 0 && (
              <section className="boost-report warning" role="alert">
                <strong>Cảnh báo khôi phục</strong>
                <ul>{boostStatus.warnings.map((warning, index) => <li key={index}>{warning}</li>)}</ul>
              </section>
            )}

            {isBoosting && boostStatus.suspendedApps.length > 0 && (
              <section className="panel suspended-panel">
                <div className="panel-header">
                  <div className="panel-header-copy">
                    <h3>Nhóm ứng dụng đang tạm dừng ({boostStatus.suspendedApps.length} nhóm · {boostSummary.instances} tiến trình)</h3>
                    <p>Bạn có thể khôi phục riêng từng ứng dụng ngay trong lúc đang chơi game.</p>
                  </div>
                </div>
                <div className="suspended-scroll">
                  {boostStatus.suspendedApps.map((proc) => (
                    <div className="process-item selected" key={proc.pid}>
                      <CheckCircle size={18} weight="fill" color="#10b981" />
                      <div className="proc-avatar">{proc.name.slice(0, 1).toUpperCase()}</div>
                      <div className="proc-main">
                        <strong>{proc.name}</strong>
                        <span>
                          PID {proc.pid} · {proc.category || 'Ứng dụng nền'}
                        </span>
                      </div>
                      <button
                        type="button"
                        className="cta-btn secondary"
                        onClick={() => void handleResumeSingle(proc.pid)}
                      >
                        Khôi phục ngay
                      </button>
                      <div className="proc-mem">{formatRAM(proc.memoryKB)}</div>
                    </div>
                  ))}
                </div>
              </section>
            )}

            <section className="panel tabbed-panel">
              <div className="chrome-tabs-bar">
                <div className="chrome-tabs">
                  <button
                    type="button"
                    className={`chrome-tab ${overviewTab === 'options' ? 'active' : ''}`}
                    onClick={() => setOverviewTab('options')}
                  >
                    <Sliders size={15} weight={overviewTab === 'options' ? 'fill' : 'regular'} />
                    <span>Tùy chọn khi Boost</span>
                  </button>
                  <button
                    type="button"
                    className={`chrome-tab ${overviewTab === 'apps' ? 'active' : ''}`}
                    onClick={() => setOverviewTab('apps')}
                  >
                    <SquaresFour size={15} weight={overviewTab === 'apps' ? 'fill' : 'regular'} />
                    <span>Ứng dụng nền</span>
                    <span className="tab-count">{selectedPIDs.size}/{processes.length}</span>
                  </button>
                </div>

                {overviewTab === 'apps' && (
                  <div className="tab-bar-actions">
                    {unknownCount > 0 && (
                      <button
                        type="button"
                        className="chip-btn kb-btn"
                        onClick={() => void openUnknownReportModal()}
                      >
                        Đóng góp {unknownCount} tiến trình lạ (Opt-in)
                      </button>
                    )}
                    <button
                      type="button"
                      className="chip-btn"
                      onClick={selectRecommended}
                    >
                      Chọn đề xuất
                    </button>
                    <button
                      type="button"
                      className="chip-btn"
                      onClick={clearSelection}
                    >
                      Bỏ chọn hết
                    </button>
                  </div>
                )}
              </div>

              {overviewTab === 'options' ? (
                <div className="tab-body-scroll">
                  <div className="option-list">
                    <div className="option-row">
                      <div className="option-left">
                        <div className="option-icon">
                          <PauseCircle size={18} />
                        </div>
                        <div className="option-copy">
                          <strong>Đóng băng & Thu hồi RAM ứng dụng nền (Suspend + EmptyWorkingSet)</strong>
                          <p>
                            Tạm dừng các tiến trình đã chọn; có thể thử gọt working set. {formatRAM(estimatedMemoryKB)} là tổng working set được ghi nhận trước Boost, không phải RAM sẽ giải phóng.
                          </p>
                        </div>
                      </div>
                      <div className="option-controls">
                        <button
                          type="button"
                          className="cta-btn secondary"
                          onClick={() => setOverviewTab('apps')}
                        >
                          Chọn ứng dụng ({selectedPIDs.size})
                        </button>
                        <Switch
                          checked={pauseApps}
                          onChange={() => {
                            const next = !pauseApps;
                            setPauseApps(next);
                            setTrimWorkingSet(next);
                          }}
                        />
                      </div>
                    </div>

                    <div className="option-row">
                      <div className="option-left">
                        <div className="option-icon">
                          <Eye size={18} />
                        </div>
                        <div className="option-copy">
                          <strong>Smart-Wake (Tự động đánh thức khi dùng & Đóng băng lại)</strong>
                          <p>
                            Theo dõi cửa sổ đang dùng: chỉ rã đông khi bạn Alt+Tab vào ứng dụng quá 250ms, và tự động đóng băng lại khi quay về game.
                          </p>
                        </div>
                      </div>
                      <div className="option-controls">
                        <Switch
                          checked={smartWake}
                          onChange={() => setSmartWake((v) => !v)}
                        />
                      </div>
                    </div>

                    <div className="option-row">
                      <div className="option-left">
                        <div className="option-icon">
                          <Lightning size={18} />
                        </div>
                        <div className="option-copy">
                          <strong>Xả bộ nhớ đệm Standby List chuẩn ISLC (NtSetSystemInformation)</strong>
                          <p>
                            Yêu cầu Windows xả standby list; kết quả phụ thuộc hệ thống, không đảm bảo giảm giật hình.
                          </p>
                        </div>
                      </div>
                      <div className="option-controls">
                        <Switch
                          checked={purgeStandby}
                          onChange={() => setPurgeStandby((v) => !v)}
                        />
                      </div>
                    </div>

                    <div className="option-row">
                      <div className="option-left">
                        <div className="option-icon">
                          <Timer size={18} />
                        </div>
                        <div className="option-copy">
                          <strong>Kernel Timer 0.5ms & MMCSS Gaming Priority</strong>
                          <p>
                            Kéo độ trễ clock kernel từ 15.6ms xuống 0.5ms và ưu tiên luồng Multimedia của Windows cho game để giảm micro-stutter.
                          </p>
                        </div>
                      </div>
                      <div className="option-controls">
                        <Switch
                          checked={timerResolution05ms}
                          onChange={() => {
                            const next = !timerResolution05ms;
                            setTimerResolution05ms(next);
                            setMmcssGpuPriority(next);
                          }}
                        />
                      </div>
                    </div>

                    <div className="option-row">
                      <div className="option-left">
                        <div className="option-icon">
                          <Gauge size={18} />
                        </div>
                        <div className="option-copy">
                          <strong>Chuyển chế độ Nguồn Hiệu năng cao (High Performance)</strong>
                          <p>
                            Ép CPU không hạ xung đột ngột giữa các pha combat nặng. Tự động trả về gói nguồn gốc khi dừng Boost.
                          </p>
                        </div>
                      </div>
                      <div className="option-controls">
                        <Switch
                          checked={highPerformance}
                          onChange={() => setHighPerformance((v) => !v)}
                        />
                      </div>
                    </div>

                    <div className="option-row">
                      <div className="option-left">
                        <div className="option-icon">
                          <ShieldCheck size={18} />
                        </div>
                        <div className="option-copy">
                          <strong>Tạm dừng dịch vụ Windows nền (Search, SysMain, Telemetry...)</strong>
                          <p>
                            Tạm dừng các dịch vụ ngốn đĩa SSD và RAM khi chơi game. Khởi động lại nguyên trạng sau khi kết thúc phiên.
                          </p>
                        </div>
                      </div>
                      <div className="option-controls">
                        <button
                          type="button"
                          className="cta-btn secondary"
                          onClick={() => setPage('settings')}
                        >
                          Tùy chỉnh dịch vụ
                        </button>
                        <Switch
                          checked={reduceWindowsTasks}
                          onChange={() => setReduceWindowsTasks((v) => !v)}
                        />
                      </div>
                    </div>

                    <div className="option-row extreme-row">
                      <div className="option-left">
                        <div className="option-icon extreme-icon">
                          <Fire size={18} weight="fill" />
                        </div>
                        <div className="option-copy">
                          <div className="option-title-line">
                            <strong>Extreme Mode I — Tắt Windows Shell (explorer.exe)</strong>
                            <span className="extreme-badge">Console Mode</span>
                          </div>
                          <p>
                            Tạm tắt thanh Taskbar & hiệu ứng kính để thu hồi RAM & VRAM. Tự động phục hồi ngay khi dừng Boost hoặc dùng phím cứu hộ.
                          </p>
                        </div>
                      </div>
                      <div className="option-controls">
                        <Switch
                          checked={extremeModeI}
                          onChange={() => setExtremeModeI((v) => !v)}
                        />
                      </div>
                    </div>

                    <div className="option-row extreme2-row">
                      <div className="option-left">
                        <div className="option-icon extreme2-icon">
                          <Fire size={18} weight="fill" />
                        </div>
                        <div className="option-copy">
                          <div className="option-title-line">
                            <strong>Extreme Mode II — Vắt kiệt tài nguyên (14 dịch vụ & Core Parking)</strong>
                            <span className="extreme2-badge">Max Power</span>
                          </div>
                          <p>
                            Bao gồm Extreme I + cắt 14 dịch vụ Windows không cần thiết, mở khóa 100% Core Parking và tự thu nhỏ bộ nhớ của chính Pulse.
                          </p>
                        </div>
                      </div>
                      <div className="option-controls">
                        <Switch
                          checked={extremeModeII}
                          onChange={() => {
                            const next = !extremeModeII;
                            setExtremeModeII(next);
                            if (next) setExtremeModeI(true);
                          }}
                        />
                      </div>
                    </div>
                  </div>
                </div>
              ) : (
                <>
                  <div className="toolbar">
                    <div className="search-box">
                      <MagnifyingGlass size={15} />
                      <input
                        type="text"
                        className="search-input"
                        placeholder="Tìm theo tên ứng dụng, phân loại hoặc PID..."
                        value={search}
                        onChange={(e) => setSearch(e.target.value)}
                      />
                    </div>
                    <div className="filter-group">
                      <button
                        type="button"
                        className={`chip-btn ${filterMode === 'all' ? 'active' : ''}`}
                        onClick={() => setFilterMode('all')}
                      >
                        Tất cả ({processes.length})
                      </button>
                      <button
                        type="button"
                        className={`chip-btn ${filterMode === 'recommended' ? 'active' : ''}`}
                        onClick={() => setFilterMode('recommended')}
                      >
                        Đề xuất ({processes.filter((p) => p.recommended).length})
                      </button>
                      <button
                        type="button"
                        className={`chip-btn ${filterMode === 'selected' ? 'active' : ''}`}
                        onClick={() => setFilterMode('selected')}
                      >
                        Đã chọn ({selectedPIDs.size})
                      </button>
                      <button
                        type="button"
                        className={`chip-btn ${filterMode === 'unknown' ? 'active' : ''}`}
                        onClick={() => setFilterMode('unknown')}
                      >
                        Chưa có trong từ điển ({unknownCount})
                      </button>
                    </div>
                  </div>

                  <div className="process-scroll">
                    {filteredProcesses.length === 0 ? (
                      <div className="empty-box">
                        Không có ứng dụng nào khớp với bộ lọc hiện tại.
                      </div>
                    ) : (
                      filteredProcesses.map((proc) => {
                        const isChecked = selectedPIDs.has(proc.pid);
                        const titleText =
                          proc.friendlyName && proc.friendlyName.toLowerCase() !== proc.name.toLowerCase()
                            ? `${proc.friendlyName} (${proc.name})`
                            : proc.name;
                        return (
                          <label
                            key={proc.pid}
                            className={`process-item ${isChecked ? 'selected' : ''}`}
                          >
                            <input
                              type="checkbox"
                              checked={isChecked}
                              onChange={() => togglePID(proc.pid)}
                            />
                            <div className="proc-avatar">
                              {(proc.friendlyName || proc.name).slice(0, 1).toUpperCase()}
                            </div>
                            <div className="proc-main">
                              <strong>
                                {titleText}
                                {proc.instanceCount > 1 ? ` · ${proc.instanceCount} tiến trình` : ''}
                              </strong>
                              <span>
                                {proc.caution
                                  ? `${proc.category} · ${proc.caution}`
                                  : `${proc.category} · ${proc.description || proc.publisher}`}
                              </span>
                            </div>
                            <div>
                              {proc.caution ? (
                                <span className="proc-tag caution">Cẩn thận Voice/Overlay</span>
                              ) : proc.recommended ? (
                                <span className="proc-tag rec">Đề xuất tạm dừng</span>
                              ) : !proc.knownInKB ? (
                                <span className="proc-tag unknown">Tự nhận diện PE</span>
                              ) : null}
                            </div>
                            <div className="proc-mem">{formatRAM(proc.memoryKB)}</div>
                          </label>
                        );
                      })
                    )}
                  </div>
                </>
              )}
            </section>
          </>
        )}

        {page === 'settings' && (
          <div className="settings-scroll">
            <header className="page-header">
              <div className="page-header-copy">
                <h1>Thiết lập nâng cao</h1>
                <p>
                  Kiểm soát chi tiết các dịch vụ Windows và cơ chế an toàn khi Owlys Pulse tối ưu hóa.
                </p>
              </div>
              <button
                type="button"
                className="cta-btn secondary"
                onClick={restoreDefaultSettings}
              >
                Khôi phục mặc định
              </button>
            </header>

            <section className="panel">
              <div className="panel-header">
                <div className="panel-header-copy">
                  <h3>Dịch vụ Windows tạm dừng khi Boost</h3>
                  <p>
                    Chỉ thử tạm dừng dịch vụ đang chạy. Dừng Boost sẽ thử khôi phục dịch vụ đã thay đổi; nếu thất bại, xem cảnh báo và thử lại.
                  </p>
                </div>
              </div>

              <div className="service-grid">
                {WINDOWS_SERVICES.map((svc) => {
                  const active = selectedServices.includes(svc.id);
                  return (
                    <label
                      key={svc.id}
                      className={`service-card ${active ? 'active' : ''}`}
                    >
                      <div>
                        <strong>{svc.name}</strong>
                        <span>{svc.desc}</span>
                      </div>
                      <input
                        type="checkbox"
                        checked={active}
                        onChange={() => toggleService(svc.id)}
                      />
                    </label>
                  );
                })}
              </div>
            </section>

            <section className="panel">
              <div className="panel-header">
                <div className="panel-header-copy">
                  <h3>Nguyên tắc an toàn của Owlys Pulse</h3>
                  <p>Thiết kế sạch, không quảng cáo, không làm hỏng phiên làm việc của bạn.</p>
                </div>
              </div>
              <div className="option-list">
                <div className="option-row">
                  <div className="option-left">
                    <div className="option-icon">
                      <ShieldCheck size={18} />
                    </div>
                    <div className="option-copy">
                      <strong>Nhật ký khôi phục khi phiên bị gián đoạn</strong>
                      <p>
                        Ghi lại thay đổi trước khi áp dụng. Khi mở lại, ứng dụng thử khôi phục phần còn lại; nếu chưa xong, Boost mới bị khóa và bạn có thể thử lại.
                      </p>
                    </div>
                  </div>
                </div>
                <div className="option-row">
                  <div className="option-left">
                    <div className="option-icon">
                      <Cpu size={18} />
                    </div>
                    <div className="option-copy">
                      <strong>Gọt working set là tùy chọn</strong>
                      <p>
                        Có thể yêu cầu gọt bộ nhớ làm việc của tiến trình được chọn. Chỉ số này không đo RAM thực sự được giải phóng hoặc hiệu năng game.
                      </p>
                    </div>
                  </div>
                </div>
              </div>
            </section>
          </div>
        )}

        {unknownReport && (
          <div className="modal-backdrop" onClick={() => setUnknownReport(null)}>
            <div className="modal-card" onClick={(e) => e.stopPropagation()}>
              <div className="panel-header">
                <div className="panel-header-copy">
                  <h3>Đóng góp tiến trình lạ vào Từ điển Owlys (Opt-in 100%)</h3>
                  <p>
                    Minh bạch tuyệt đối: Bạn kiểm tra toàn bộ nội dung JSON bên dưới trước khi lưu hoặc gửi. Không chứa đường dẫn cá nhân hay tên người dùng.
                  </p>
                </div>
                <button
                  type="button"
                  className="modal-close-btn"
                  onClick={() => setUnknownReport(null)}
                >
                  ✕
                </button>
              </div>
              <div className="modal-body">
                {savedNotice && <div className="notice-banner">{savedNotice}</div>}
                <pre className="json-preview">{unknownReport.prettyJSON}</pre>
              </div>
              <div className="panel-footer">
                <span>{unknownReport.totalUnknown} tiến trình chưa có trong từ điển tĩnh.</span>
                <div className="filter-group">
                  <button
                    type="button"
                    className="cta-btn secondary"
                    onClick={() => setUnknownReport(null)}
                  >
                    Đóng
                  </button>
                  <button
                    type="button"
                    className="cta-btn primary"
                    onClick={() => void handleExportUnknownReport()}
                  >
                    Lưu JSON & Sao chép
                  </button>
                </div>
              </div>
            </div>
          </div>
        )}
      </main>
    </div>
  );
}

function Switch({checked, onChange, disabled = false}: {checked: boolean; onChange: () => void; disabled?: boolean}) {
  return (
    <button
      type="button"
      role="switch"
      aria-checked={checked}
      aria-label={disabled ? 'Tùy chọn chưa hỗ trợ' : 'Bật hoặc tắt tùy chọn'}
      disabled={disabled}
      className={`switch ${checked ? 'checked' : ''}`}
      onClick={onChange}
    >
      <span className="switch-thumb" />
    </button>
  );
}

export default App;
