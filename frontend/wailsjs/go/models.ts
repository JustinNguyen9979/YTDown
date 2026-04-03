export namespace main {
	
	export class CompressionOptions {
	    type: string;
	    quality: string;
	    format: string;
	    savePath: string;
	
	    static createFrom(source: any = {}) {
	        return new CompressionOptions(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.type = source["type"];
	        this.quality = source["quality"];
	        this.format = source["format"];
	        this.savePath = source["savePath"];
	    }
	}

}

