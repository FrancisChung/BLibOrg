export namespace appapi {
	
	export class ApplyResultEntry {
	    sourcePath: string;
	    ok: boolean;
	    error: string;
	    skipped: boolean;
	
	    static createFrom(source: any = {}) {
	        return new ApplyResultEntry(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.sourcePath = source["sourcePath"];
	        this.ok = source["ok"];
	        this.error = source["error"];
	        this.skipped = source["skipped"];
	    }
	}
	export class ApplyResult {
	    batchId: string;
	    results: ApplyResultEntry[];
	
	    static createFrom(source: any = {}) {
	        return new ApplyResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.batchId = source["batchId"];
	        this.results = this.convertValues(source["results"], ApplyResultEntry);
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
	
	export class Field {
	    value: string;
	    source: string;
	
	    static createFrom(source: any = {}) {
	        return new Field(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.value = source["value"];
	        this.source = source["source"];
	    }
	}
	export class BookView {
	    id: string;
	    sourcePath: string;
	    oldFilename: string;
	    format: string;
	    sizeBytes: number;
	    subject: string;
	    title: Field;
	    author: Field;
	    year: Field;
	    status: string;
	    category: string;
	    subcategory: string;
	    categoryWarning: string;
	    destPath: string;
	    duplicateStatus: string;
	    duplicateGroupId: string;
	
	    static createFrom(source: any = {}) {
	        return new BookView(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.sourcePath = source["sourcePath"];
	        this.oldFilename = source["oldFilename"];
	        this.format = source["format"];
	        this.sizeBytes = source["sizeBytes"];
	        this.subject = source["subject"];
	        this.title = this.convertValues(source["title"], Field);
	        this.author = this.convertValues(source["author"], Field);
	        this.year = this.convertValues(source["year"], Field);
	        this.status = source["status"];
	        this.category = source["category"];
	        this.subcategory = source["subcategory"];
	        this.categoryWarning = source["categoryWarning"];
	        this.destPath = source["destPath"];
	        this.duplicateStatus = source["duplicateStatus"];
	        this.duplicateGroupId = source["duplicateGroupId"];
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
	export class CategoryWarningView {
	    timestamp: string;
	    sourcePath: string;
	    category: string;
	    subcategory: string;
	    warning: string;
	
	    static createFrom(source: any = {}) {
	        return new CategoryWarningView(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.timestamp = source["timestamp"];
	        this.sourcePath = source["sourcePath"];
	        this.category = source["category"];
	        this.subcategory = source["subcategory"];
	        this.warning = source["warning"];
	    }
	}
	export class ConfigStatusView {
	    path: string;
	    error: string;
	    warnings: string[];
	
	    static createFrom(source: any = {}) {
	        return new ConfigStatusView(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.path = source["path"];
	        this.error = source["error"];
	        this.warnings = source["warnings"];
	    }
	}
	
	export class OperationEntryView {
	    oldPath: string;
	    newPath: string;
	    opType: string;
	    undone: boolean;
	
	    static createFrom(source: any = {}) {
	        return new OperationEntryView(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.oldPath = source["oldPath"];
	        this.newPath = source["newPath"];
	        this.opType = source["opType"];
	        this.undone = source["undone"];
	    }
	}
	export class OperationBatchView {
	    batchId: string;
	    timestamp: string;
	    entryCount: number;
	    undoneCount: number;
	    entries: OperationEntryView[];
	
	    static createFrom(source: any = {}) {
	        return new OperationBatchView(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.batchId = source["batchId"];
	        this.timestamp = source["timestamp"];
	        this.entryCount = source["entryCount"];
	        this.undoneCount = source["undoneCount"];
	        this.entries = this.convertValues(source["entries"], OperationEntryView);
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

