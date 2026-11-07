export namespace main {
	
	export class EditOptions {
	    package: string;
	    name: string;
	    publisher: string;
	    displayVersion: string;
	    icon: string;
	    langNames: Record<string, string>;
	    langIcons: Record<string, string>;
	    clearName: boolean;
	    clearPublisher: boolean;
	    clearDisplay: boolean;
	    clearIcon: boolean;
	
	    static createFrom(source: any = {}) {
	        return new EditOptions(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.package = source["package"];
	        this.name = source["name"];
	        this.publisher = source["publisher"];
	        this.displayVersion = source["displayVersion"];
	        this.icon = source["icon"];
	        this.langNames = source["langNames"];
	        this.langIcons = source["langIcons"];
	        this.clearName = source["clearName"];
	        this.clearPublisher = source["clearPublisher"];
	        this.clearDisplay = source["clearDisplay"];
	        this.clearIcon = source["clearIcon"];
	    }
	}
	export class PackOptions {
	    level: number;
	    titleId: string;
	    displayVersion: string;
	    publisher: string;
	    name: string;
	    langNames: Record<string, string>;
	    icon: string;
	    langIcons: Record<string, string>;
	    keepListings: boolean;
	    quiet: boolean;
	
	    static createFrom(source: any = {}) {
	        return new PackOptions(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.level = source["level"];
	        this.titleId = source["titleId"];
	        this.displayVersion = source["displayVersion"];
	        this.publisher = source["publisher"];
	        this.name = source["name"];
	        this.langNames = source["langNames"];
	        this.icon = source["icon"];
	        this.langIcons = source["langIcons"];
	        this.keepListings = source["keepListings"];
	        this.quiet = source["quiet"];
	    }
	}

}

