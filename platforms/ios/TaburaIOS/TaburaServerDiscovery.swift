import Foundation

final class TaburaServerDiscovery: NSObject, ObservableObject, NetServiceBrowserDelegate, NetServiceDelegate {
    @Published private(set) var servers: [TaburaDiscoveredServer] = []

    private var browser: NetServiceBrowser?
    private var discovered: [String: TaburaDiscoveredServer] = [:]
    private var services: [String: NetService] = [:]

    func start() {
        if browser != nil {
            return
        }
        let browser = NetServiceBrowser()
        browser.delegate = self
        self.browser = browser
        browser.searchForServices(ofType: "_tabura._tcp.", inDomain: "local.")
    }

    func stop() {
        browser?.stop()
        browser = nil
        discovered = [:]
        services = [:]
        servers = []
    }

    func netServiceBrowser(_ browser: NetServiceBrowser, didFind service: NetService, moreComing: Bool) {
        let key = "\(service.name).\(service.type)\(service.domain)"
        services[key] = service
        service.delegate = self
        service.resolve(withTimeout: 5)
        if !moreComing {
            publishServers()
        }
    }

    func netServiceBrowser(_ browser: NetServiceBrowser, didRemove service: NetService, moreComing: Bool) {
        let key = "\(service.name).\(service.type)\(service.domain)"
        services.removeValue(forKey: key)
        discovered = discovered.filter { _, value in value.name != service.name }
        if !moreComing {
            publishServers()
        }
    }

    func netServiceDidResolveAddress(_ sender: NetService) {
        guard let host = sender.hostName?.trimmingCharacters(in: .whitespacesAndNewlines), host.isEmpty == false else {
            return
        }
        let cleanHost = host.hasSuffix(".") ? String(host.dropLast()) : host
        let id = "\(sender.name)-\(cleanHost)-\(sender.port)"
        discovered[id] = TaburaDiscoveredServer(id: id, name: sender.name, host: cleanHost, port: sender.port)
        publishServers()
    }

    func netService(_ sender: NetService, didNotResolve errorDict: [String : NSNumber]) {
        _ = sender
        _ = errorDict
    }

    private func publishServers() {
        servers = discovered.values.sorted { $0.name.localizedCaseInsensitiveCompare($1.name) == .orderedAscending }
    }
}
