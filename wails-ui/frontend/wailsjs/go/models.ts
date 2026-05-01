export namespace main {
	
	export class ProxyEntry {
	    name: string;
	    type: string;
	    localPort: number;
	    remotePort: number;
	
	    static createFrom(source: any = {}) {
	        return new ProxyEntry(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.type = source["type"];
	        this.localPort = source["localPort"];
	        this.remotePort = source["remotePort"];
	    }
	}
	export class ConnectionSettings {
	    serverAddr: string;
	    serverPort: number;
	    authToken: string;
	    proxies: ProxyEntry[];
	
	    static createFrom(source: any = {}) {
	        return new ConnectionSettings(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.serverAddr = source["serverAddr"];
	        this.serverPort = source["serverPort"];
	        this.authToken = source["authToken"];
	        this.proxies = this.convertValues(source["proxies"], ProxyEntry);
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
	
	export class StatusView {
	    serviceInstalled: boolean;
	    serviceRunning: boolean;
	    serviceAutoStart: boolean;
	    proxyRunning: boolean;
	    headerText: string;
	    headerTone: string;
	    serviceText: string;
	    serviceDetail: string;
	    proxyText: string;
	    proxyDetail: string;
	    silentStartAtLogin: boolean;
	    configPath: string;
	
	    static createFrom(source: any = {}) {
	        return new StatusView(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.serviceInstalled = source["serviceInstalled"];
	        this.serviceRunning = source["serviceRunning"];
	        this.serviceAutoStart = source["serviceAutoStart"];
	        this.proxyRunning = source["proxyRunning"];
	        this.headerText = source["headerText"];
	        this.headerTone = source["headerTone"];
	        this.serviceText = source["serviceText"];
	        this.serviceDetail = source["serviceDetail"];
	        this.proxyText = source["proxyText"];
	        this.proxyDetail = source["proxyDetail"];
	        this.silentStartAtLogin = source["silentStartAtLogin"];
	        this.configPath = source["configPath"];
	    }
	}
	export class Snapshot {
	    settings: ConnectionSettings;
	    status: StatusView;
	    logs: string[];
	
	    static createFrom(source: any = {}) {
	        return new Snapshot(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.settings = this.convertValues(source["settings"], ConnectionSettings);
	        this.status = this.convertValues(source["status"], StatusView);
	        this.logs = source["logs"];
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

