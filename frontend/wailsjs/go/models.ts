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

