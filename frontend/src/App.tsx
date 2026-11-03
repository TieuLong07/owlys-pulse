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
import './App.css';

type Page = 'overview' | 'settings';
type OverviewTab = 'options' | 'apps';
type FilterMode = 'all' | 'recommended' | 'selected' | 'unknown';

const WINDOWS_SERVICES = [
  {
    id: 'WSearch',
    name: 'Windows Search (WSearch)',
    desc: 'Tạm dừng lập chỉ mục tìm kiếm ổ đĩa trong phiên chơi game.',
  },
  {
    id: 'SysMain',
    name: 'SysMain (Superfetch)',
    desc: 'Giảm đọc/ghi bộ nhớ đệm nền khi game đang tải map lớn.',
  },
  {
    id: 'DiagTrack',
    name: 'Connected User Experiences (DiagTrack)',
    desc: 'Tạm ngưng thu thập chẩn đoán nền của Windows.',
  },
  {
    id: 'Spooler',
    name: 'Print Spooler',
    desc: 'Tạm dừng dịch vụ máy in nếu bạn không in ấn khi chơi.',
  },
];

function formatRAM(kb: number): string {
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
  const [autoSeeded, setAutoSeeded] = useState(false);
  const [loadingProcs, setLoadingProcs] = useState(false);
  const [busyBoost, setBusyBoost] = useState(false);
  const [errorMsg, setErrorMsg] = useState('');
  const [search, setSearch] = useState('');
  const [filterMode, setFilterMode] = useState<FilterMode>('all');
  const [unknownReport, setUnknownReport] = useState<main.UnknownProcessReport | null>(null);
  const [savedNotice, setSavedNotice] = useState('');

  // Main Boost Options
  const [pauseApps, setPauseApps] = useState(true);
  const [smartWake, setSmartWake] = useState(true);
  const [trimWorkingSet, setTrimWorkingSet] = useState(true);
  const [purgeStandby, setPurgeStandby] = useState(true);
  const [timerResolution05ms, setTimerResolution05ms] = useState(true);
  const [mmcssGpuPriority, setMmcssGpuPriority] = useState(true);
  const [reduceWindowsTasks, setReduceWindowsTasks] = useState(true);
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
      await loadSystemState(true);
    } catch {
      setErrorMsg('Không thể kích hoạt Game Boost. Vui lòng thử lại.');
    } finally {
      setBusyBoost(false);
    }
  };

  const handleStopBoost = async () => {
    setBusyBoost(true);
    setErrorMsg('');
    try {
      const status = await StopBoost();
      setBoostStatus(status);
      await loadSystemState(true);
    } catch {
      setErrorMsg('Không thể khôi phục trạng thái phiên boost.');
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
    setSmartWake(true);
    setTrimWorkingSet(true);
    setPurgeStandby(true);
    setTimerResolution05ms(true);
    setMmcssGpuPriority(true);
    setReduceWindowsTasks(true);
    setHighPerformance(true);
    setExtremeModeI(false);
    setExtremeModeII(false);
    setSelectedServices(['WSearch', 'SysMain', 'DiagTrack']);
    selectRecommended();
  };

  const isBoosting = boostStatus.active;

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
            <span>{isBoosting ? 'Đang tối ưu hệ thống' : 'Sẵn sàng tăng tốc'}</span>
          </div>
          <p>
            {isBoosting
              ? 'Mọi thay đổi sẽ tự động hoàn tác khi bạn tắt Boost hoặc đóng ứng dụng.'
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
                <p>Giải phóng tài nguyên nền và ưu tiên hiệu năng CPU ngay khi bạn vào game.</p>
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
                        ? `Đã đóng băng ${boostStatus.suspendedApps.length} ứng dụng nền và chuyển nguồn điện sang hiệu năng cao.`
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
                    disabled={busyBoost}
                    onClick={() => void handleStartBoost()}
                  >
                    <Lightning size={18} weight="fill" />
                    <span>{busyBoost ? 'Đang kích hoạt...' : 'Bật Game Boost ngay'}</span>
                  </button>
                )}
              </div>

              <div className="hero-metrics">
                <div className="metric-item">
                  <span>Ứng dụng nền tạm dừng</span>
                  <strong>
                    {isBoosting
                      ? `${boostStatus.suspendedApps.length} đang tạm dừng`
                      : pauseApps
                        ? `${selectedPIDs.size} ứng dụng đã chọn`
                        : 'Đang tắt'}
                  </strong>
                </div>
                <div className="metric-item">
                  <span>Bộ nhớ nền giải tỏa</span>
                  <strong>
                    {isBoosting
                      ? formatRAM(boostStatus.savedMemoryKB)
                      : pauseApps
                        ? `~${formatRAM(estimatedMemoryKB)}`
                        : '0 MB'}
                  </strong>
                </div>
                <div className="metric-item">
                  <span>Chế độ tối ưu Kernel</span>
                  <strong>
                    {isBoosting
                      ? boostStatus.extremeIIActive
                        ? 'Extreme II (Vắt kiệt)'
                        : boostStatus.extremeIActive
                          ? 'Extreme I (Tắt Shell)'
                          : 'Chuẩn + Timer 0.5ms'
                      : extremeModeII
                        ? 'Extreme II (Vắt kiệt)'
                        : extremeModeI
                          ? 'Extreme I (Tắt Shell)'
                          : timerResolution05ms
                            ? 'Timer 0.5ms + ISLC'
                            : 'Tiêu chuẩn'}
                  </strong>
                </div>
              </div>
            </section>

            {isBoosting && boostStatus.suspendedApps.length > 0 && (
              <section className="panel suspended-panel">
                <div className="panel-header">
                  <div className="panel-header-copy">
                    <h3>Ứng dụng đang tạm dừng ({boostStatus.suspendedApps.length})</h3>
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
                            Đóng băng CPU về 0% cho {selectedPIDs.size} ứng dụng đã chọn ({formatRAM(estimatedMemoryKB)}) và ép rút toàn bộ Working Set xuống Pagefile.
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
                          <strong>Smart-Wake chống đánh thức bừa bãi (Dwell Filter + Auto Re-Freeze)</strong>
                          <p>
                            Chỉ rã đông khi bạn cố tình click hoặc Alt+Tab vào app quá 250ms (chặn hoàn toàn IPC nền gọi ngầm). Tự động đóng băng + gọt RAM lại khi quay về game.
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
                            Dọn sạch các trang bộ nhớ đệm bị Windows giữ lì trong RAM để chống khựng khung hình (stutter) trên máy ít RAM.
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
                          <strong>Ép xung nhịp bộ đếm Kernel 0.5ms & Ưu tiên GPU/MMCSS</strong>
                          <p>
                            Hạ Timer Resolution từ 15.6ms xuống 0.5ms, đặt GPU Priority = 8, tắt Network Throttling và ưu tiên High Performance.
                          </p>
                        </div>
                      </div>
                      <div className="option-controls">
                        <Switch
                          checked={timerResolution05ms && mmcssGpuPriority}
                          onChange={() => {
                            const next = !(timerResolution05ms && mmcssGpuPriority);
                            setTimerResolution05ms(next);
                            setMmcssGpuPriority(next);
                            setHighPerformance(next);
                          }}
                        />
                      </div>
                    </div>

                    <div className="option-row">
                      <div className="option-left">
                        <div className="option-icon">
                          <ShieldCheck size={18} />
                        </div>
                        <div className="option-copy">
                          <strong>Giảm tác vụ & dịch vụ Windows nền</strong>
                          <p>
                            Tạm dừng {selectedServices.length} dịch vụ ngốn I/O đĩa ({selectedServices.join(', ')}) và bật lại sau khi thoát.
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
                            <strong>Extreme Mode I — Tắt Windows Shell (explorer.exe) & Hiệu ứng kính</strong>
                            <span className="extreme-badge">Máy yếu</span>
                          </div>
                          <p>
                            Tạm đóng màn hình nền, thanh Taskbar và hiệu ứng trong suốt để thu hồi ~200MB RAM + VRAM. Tự mở lại Explorer khi dừng Boost.
                          </p>
                        </div>
                      </div>
                      <div className="option-controls">
                        <Switch
                          checked={extremeModeI}
                          onChange={() => {
                            const next = !extremeModeI;
                            setExtremeModeI(next);
                            if (!next) setExtremeModeII(false);
                          }}
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
                            <strong>Extreme Mode II — Vắt kiệt phần cứng (Zero-OS Overhead)</strong>
                            <span className="extreme2-badge">Siêu cực đoan</span>
                          </div>
                          <p>
                            Bao gồm Extreme I + cắt thêm 14 dịch vụ nền Windows, mở khóa 100% Core Parking, nâng I/O + Page Priority cho game và tự ép nhỏ RAM của chính Owlys Pulse.
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
                    Chỉ những dịch vụ đang chạy mới được tạm dừng và tự động khởi động lại sau khi tắt Boost.
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
                      <strong>Tự động khôi phục khi thoát đột ngột (Crash-Safe Snapshot)</strong>
                      <p>
                        Trạng thái tiến trình và Power Plan gốc được lưu cục bộ. Nếu máy mất điện hoặc đóng app, lần mở tiếp theo sẽ tự khôi phục.
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
                      <strong>Không xả RAM ảo (No Working-Set Flush)</strong>
                      <p>
                        Owlys Pulse không ép xóa bộ nhớ làm việc gây giật lag page-fault như các phần mềm dọn RAM cũ.
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

function Switch({checked, onChange}: {checked: boolean; onChange: () => void}) {
  return (
    <button
      type="button"
      role="switch"
      aria-checked={checked}
      aria-label="Bật hoặc tắt tùy chọn"
      className={`switch ${checked ? 'checked' : ''}`}
      onClick={onChange}
    >
      <span className="switch-thumb" />
    </button>
  );
}

export default App;
