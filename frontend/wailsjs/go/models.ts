export namespace main {
	
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

}

