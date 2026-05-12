export namespace main {
	
	export class AppInfo {
	    name: string;
	    version: string;
	    author: string;
	
	    static createFrom(source: any = {}) {
	        return new AppInfo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.version = source["version"];
	        this.author = source["author"];
	    }
	}
	export class AppUpdateInfo {
	    current: string;
	    latest: string;
	    available: boolean;
	
	    static createFrom(source: any = {}) {
	        return new AppUpdateInfo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.current = source["current"];
	        this.latest = source["latest"];
	        this.available = source["available"];
	    }
	}
	export class BinaryVersion {
	    name: string;
	    current: string;
	    latest: string;
	    canUpgrade: boolean;
	    updatePath: string;
	
	    static createFrom(source: any = {}) {
	        return new BinaryVersion(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.current = source["current"];
	        this.latest = source["latest"];
	        this.canUpgrade = source["canUpgrade"];
	        this.updatePath = source["updatePath"];
	    }
	}
	export class CompressionOptions {
	    type: string;
	    quality: string;
	    customQuality: number;
	    useSlowPreset: boolean;
	    format: string;
	    savePath: string;
	
	    static createFrom(source: any = {}) {
	        return new CompressionOptions(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.type = source["type"];
	        this.quality = source["quality"];
	        this.customQuality = source["customQuality"];
	        this.useSlowPreset = source["useSlowPreset"];
	        this.format = source["format"];
	        this.savePath = source["savePath"];
	    }
	}
	export class CookieConfig {
	    mode: string;
	    selected_browser: string;
	    manual_header?: string;
	
	    static createFrom(source: any = {}) {
	        return new CookieConfig(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.mode = source["mode"];
	        this.selected_browser = source["selected_browser"];
	        this.manual_header = source["manual_header"];
	    }
	}
	export class DependencyStatus {
	    name: string;
	    installed: boolean;
	    version: string;
	    error: string;
	
	    static createFrom(source: any = {}) {
	        return new DependencyStatus(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.installed = source["installed"];
	        this.version = source["version"];
	        this.error = source["error"];
	    }
	}
	export class DependencyCheckResult {
	    allInstalled: boolean;
	    dependencies: DependencyStatus[];
	    missingTools: string[];
	    errorMessage: string;
	
	    static createFrom(source: any = {}) {
	        return new DependencyCheckResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.allInstalled = source["allInstalled"];
	        this.dependencies = this.convertValues(source["dependencies"], DependencyStatus);
	        this.missingTools = source["missingTools"];
	        this.errorMessage = source["errorMessage"];
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
	
	export class GalleryDownloadOptions {
	    savePath: string;
	    threads: number;
	    ugoiraToWebm: boolean;
	    formats: string[];
	    allFormats: boolean;
	    archive: boolean;
	    extraArgs: string;
	
	    static createFrom(source: any = {}) {
	        return new GalleryDownloadOptions(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.savePath = source["savePath"];
	        this.threads = source["threads"];
	        this.ugoiraToWebm = source["ugoiraToWebm"];
	        this.formats = source["formats"];
	        this.allFormats = source["allFormats"];
	        this.archive = source["archive"];
	        this.extraArgs = source["extraArgs"];
	    }
	}
	export class VideoInfo {
	    title: string;
	    thumbnail: string;
	    id: string;
	
	    static createFrom(source: any = {}) {
	        return new VideoInfo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.title = source["title"];
	        this.thumbnail = source["thumbnail"];
	        this.id = source["id"];
	    }
	}

}

