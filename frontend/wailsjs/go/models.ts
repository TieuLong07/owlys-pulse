export namespace main {
	
	export class BoostRequest {
	    pauseApps: boolean;
	    selectedPIDs: number[];
	    smartWake: boolean;
	    trimWorkingSet: boolean;
	    purgeStandby: boolean;
	    timerResolution05ms: boolean;
	    mmcssGpuPriority: boolean;
	    reduceWindowsTasks: boolean;
	    serviceNames: string[];
	    highPerformance: boolean;
	    extremeModeI: boolean;
	    extremeModeII: boolean;
	
	    static createFrom(source: any = {}) {
	        return new BoostRequest(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.pauseApps = source["pauseApps"];
	        this.selectedPIDs = source["selectedPIDs"];
	        this.smartWake = source["smartWake"];
	        this.trimWorkingSet = source["trimWorkingSet"];
	        this.purgeStandby = source["purgeStandby"];
	        this.timerResolution05ms = source["timerResolution05ms"];
	        this.mmcssGpuPriority = source["mmcssGpuPriority"];
	        this.reduceWindowsTasks = source["reduceWindowsTasks"];
	        this.serviceNames = source["serviceNames"];
	        this.highPerformance = source["highPerformance"];
	        this.extremeModeI = source["extremeModeI"];
	        this.extremeModeII = source["extremeModeII"];
	    }
	}
	export class RunningProcess {
	    name: string;
	    friendlyName: string;
	    publisher: string;
	    description: string;
	    knownInKB: boolean;
	    pid: number;
	    pids: number[];
	    instanceCount: number;
	    memoryKB: number;
	    sessionName: string;
	    category: string;
	    recommended: boolean;
	    caution: string;
	
	    static createFrom(source: any = {}) {
	        return new RunningProcess(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.friendlyName = source["friendlyName"];
	        this.publisher = source["publisher"];
	        this.description = source["description"];
	        this.knownInKB = source["knownInKB"];
	        this.pid = source["pid"];
	        this.pids = source["pids"];
	        this.instanceCount = source["instanceCount"];
	        this.memoryKB = source["memoryKB"];
	        this.sessionName = source["sessionName"];
	        this.category = source["category"];
	        this.recommended = source["recommended"];
	        this.caution = source["caution"];
	    }
	}
	export class BoostStatus {
	    active: boolean;
	    startedAt: string;
	    suspendedApps: RunningProcess[];
	    savedMemoryKB: number;
	    trimmedProcessCount: number;
	    smartWakeActive: boolean;
	    standbyPurged: boolean;
	    timer05msActive: boolean;
	    mmcssActive: boolean;
	    stoppedServices: string[];
	    powerPlanSwitched: boolean;
	    originalPowerScheme: string;
	    extremeIActive: boolean;
	    originalTransp: number;
	    extremeIIActive: boolean;
	    extremeIIServices: string[];
	    warnings: string[];
	
	    static createFrom(source: any = {}) {
	        return new BoostStatus(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.active = source["active"];
	        this.startedAt = source["startedAt"];
	        this.suspendedApps = this.convertValues(source["suspendedApps"], RunningProcess);
	        this.savedMemoryKB = source["savedMemoryKB"];
	        this.trimmedProcessCount = source["trimmedProcessCount"];
	        this.smartWakeActive = source["smartWakeActive"];
	        this.standbyPurged = source["standbyPurged"];
	        this.timer05msActive = source["timer05msActive"];
	        this.mmcssActive = source["mmcssActive"];
	        this.stoppedServices = source["stoppedServices"];
	        this.powerPlanSwitched = source["powerPlanSwitched"];
	        this.originalPowerScheme = source["originalPowerScheme"];
	        this.extremeIActive = source["extremeIActive"];
	        this.originalTransp = source["originalTransp"];
	        this.extremeIIActive = source["extremeIIActive"];
	        this.extremeIIServices = source["extremeIIServices"];
	        this.warnings = source["warnings"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	
	export class UnknownProcessItem {
	    exeName: string;
	    publisher: string;
	    fileDescription: string;
	    instanceCount: number;
	    memoryMB: number;
	
	    static createFrom(source: any = {}) {
	        return new UnknownProcessItem(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.exeName = source["exeName"];
	        this.publisher = source["publisher"];
	        this.fileDescription = source["fileDescription"];
	        this.instanceCount = source["instanceCount"];
	        this.memoryMB = source["memoryMB"];
	    }
	}
	export class UnknownProcessReport {
	    generatedAt: string;
	    privacyNote: string;
	    totalUnknown: number;
	    savedPath: string;
	    prettyJSON: string;
	    items: UnknownProcessItem[];
	
	    static createFrom(source: any = {}) {
	        return new UnknownProcessReport(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.generatedAt = source["generatedAt"];
	        this.privacyNote = source["privacyNote"];
	        this.totalUnknown = source["totalUnknown"];
	        this.savedPath = source["savedPath"];
	        this.prettyJSON = source["prettyJSON"];
	        this.items = this.convertValues(source["items"], UnknownProcessItem);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}

}

