// MIMUS Menu: l'icona di MIMUS nella barra dei menu di macOS.
//
// Il dinosauro dorme (Z Z Z) quando è tutto a posto o si lavora offline,
// corre mentre sincronizza, mostra «!» quando c'è un problema.
// Le immagini sono "template": macOS le colora come le icone di sistema.
// Lo stato arriva dal servizio di MIMUS (rclone, rc su 127.0.0.1:5572):
// mimus/status, vfs/stats, core/stats. Avviata da un LaunchAgent
// (app.mimus.menu) che scrive l'app MIMUS.
//
//   MIMUSMenu --selftest   controlla che le immagini ci siano (per la CI)
import Cocoa

let rcBase = "http://127.0.0.1:5572/"
let pollSeconds = 2.0
let runFrames = ["run-0", "run-1", "run-2", "run-3"]
let sleepFrames = ["sleep-0", "sleep-1", "sleep-2", "sleep-2"]
let allFrames = runFrames + ["sleep-0", "sleep-1", "sleep-2", "alert"]
// MIMUS_MENU_DEBUG=1: scrive su stderr ogni cambio di stato (per i test)
let debug = ProcessInfo.processInfo.environment["MIMUS_MENU_DEBUG"] != nil

func num(_ v: Any?) -> Int { return (v as? NSNumber)?.intValue ?? 0 }
func flag(_ v: Any?) -> Bool { return (v as? NSNumber)?.boolValue ?? false }

func loadFrame(_ name: String) -> NSImage? {
    guard let img = Bundle.main.image(forResource: "menubar-" + name) else { return nil }
    img.isTemplate = true
    img.size = NSSize(width: 24, height: 18)
    return img
}

// cartelle montate, senza toccare i dischi (MNT_NOWAIT: nessun blocco se la rete è giù)
func mountPoints() -> Set<String> {
    var out = Set<String>()
    var buf: UnsafeMutablePointer<statfs>? = nil
    let n = Int(getmntinfo(&buf, MNT_NOWAIT))
    guard n > 0, let list = buf else { return out }
    for i in 0..<n {
        var name = list[i].f_mntonname
        let path = withUnsafeBytes(of: &name) { raw -> String in
            String(decoding: raw.prefix(while: { $0 != 0 }), as: UTF8.self)
        }
        out.insert(path)
    }
    return out
}

func readSettings() -> [String: Any]? {
    let path = NSHomeDirectory() + "/Library/Application Support/MIMUS/settings.json"
    guard let data = FileManager.default.contents(atPath: path),
          let obj = try? JSONSerialization.jsonObject(with: data, options: []),
          let dict = obj as? [String: Any] else { return nil }
    return dict
}

// ---------- lettura dello stato (thread in background) ----------

final class Snapshot: NSObject {
    var rcUp = false
    var status: [String: Any] = [:]
    var vfs: [String: Any] = [:]
    var core: [String: Any] = [:]
    var refreshing = false
    var mounts = Set<String>()
    var transfers: [[String: Any]] = []
}

final class Box: @unchecked Sendable {
    var value: [String: Any]? = nil
}

final class Poller: NSObject {
    let session: URLSession
    weak var receiver: NSObject?
    let lock = NSLock()
    var refreshJob: Int? = nil

    override init() {
        let cfg = URLSessionConfiguration.ephemeral
        cfg.timeoutIntervalForRequest = 3
        cfg.timeoutIntervalForResource = 5
        cfg.connectionProxyDictionary = [:] // mai attraverso un proxy
        session = URLSession(configuration: cfg)
        super.init()
    }

    // chiamata rc sincrona; nil se il servizio non risponde
    func call(_ path: String, _ params: [String: Any] = [:]) -> [String: Any]? {
        guard let url = URL(string: rcBase + path) else { return nil }
        var req = URLRequest(url: url)
        req.httpMethod = "POST"
        req.setValue("application/json", forHTTPHeaderField: "Content-Type")
        req.httpBody = try? JSONSerialization.data(withJSONObject: params, options: [])
        let box = Box()
        let done = DispatchSemaphore(value: 0)
        let task = session.dataTask(with: req) { data, resp, _ in
            if let http = resp as? HTTPURLResponse, http.statusCode == 200, let data = data,
               let obj = try? JSONSerialization.jsonObject(with: data, options: []) as? [String: Any] {
                box.value = obj
            }
            done.signal()
        }
        task.resume()
        if done.wait(timeout: .now() + 6) == .timedOut {
            task.cancel()
            return nil
        }
        return box.value
    }

    func startRefresh() {
        let r = call("vfs/refresh", ["recursive": true, "_async": true])
        lock.lock()
        if let r = r { refreshJob = num(r["jobid"]) }
        lock.unlock()
    }

    func snapshot() -> Snapshot {
        let s = Snapshot()
        guard let st = call("mimus/status") else { return s }
        s.rcUp = true
        s.status = st
        s.vfs = call("vfs/stats") ?? [:]
        s.core = call("core/stats") ?? [:]
        s.transfers = (call("vfs/mimus-transfers")?["transfers"] as? [[String: Any]]) ?? []
        lock.lock()
        let job = refreshJob
        lock.unlock()
        if let job = job {
            let js = call("job/status", ["jobid": job])
            if let js = js, !flag(js["finished"]) {
                s.refreshing = true
            } else {
                lock.lock()
                refreshJob = nil
                lock.unlock()
            }
        }
        s.mounts = mountPoints()
        return s
    }

    @objc func loop() {
        while true {
            var moving = false
            autoreleasepool {
                let s = snapshot()
                moving = !s.transfers.isEmpty
                receiver?.performSelector(onMainThread: #selector(MenuController.apply(_:)), with: s, waitUntilDone: false)
            }
            // con file in movimento la barra si aggiorna ogni secondo
            Thread.sleep(forTimeInterval: moving ? 1 : pollSeconds)
        }
    }
}

// ---------- avanzamento di un file (riga del menu) ----------

func bytesText(_ n: Int64) -> String {
    return ByteCountFormatter.string(fromByteCount: n, countStyle: .file)
}

func etaText(_ seconds: Double) -> String {
    if seconds < 60 { return "ancora \(max(1, Int(seconds.rounded()))) s" }
    if seconds < 3600 { return "ancora \(Int((seconds / 60).rounded())) min" }
    return String(format: "ancora %.1f h", seconds / 3600)
}

final class TransferRow: NSView {
    let name = NSTextField(labelWithString: "")
    let bar = NSProgressIndicator()
    let info = NSTextField(labelWithString: "")

    init() {
        super.init(frame: NSRect(x: 0, y: 0, width: 320, height: 50))
        name.font = NSFont.menuFont(ofSize: 13)
        name.lineBreakMode = .byTruncatingMiddle
        name.frame = NSRect(x: 18, y: 29, width: 290, height: 17)
        bar.style = .bar
        bar.isIndeterminate = false
        bar.minValue = 0
        bar.maxValue = 1
        bar.controlSize = .small
        bar.frame = NSRect(x: 18, y: 17, width: 290, height: 10)
        info.font = NSFont.systemFont(ofSize: 11)
        info.textColor = .secondaryLabelColor
        info.lineBreakMode = .byTruncatingTail
        info.frame = NSRect(x: 18, y: 2, width: 290, height: 14)
        addSubview(name)
        addSubview(bar)
        addSubview(info)
    }

    required init?(coder: NSCoder) { fatalError("init(coder:) non usato") }

    func update(_ t: [String: Any]) {
        let path = (t["name"] as? String) ?? ""
        let up = flag(t["up"])
        let size = (t["size"] as? NSNumber)?.int64Value ?? 0
        let done = (t["done"] as? NSNumber)?.int64Value ?? 0
        let speed = (t["speed"] as? NSNumber)?.doubleValue ?? 0
        name.stringValue = (up ? "↑ " : "↓ ") + (path as NSString).lastPathComponent
        name.toolTip = path
        let frac = size > 0 ? min(1, Double(done) / Double(size)) : 0
        bar.doubleValue = frac
        var parts = ["\(Int(frac * 100))%", "\(bytesText(done)) di \(bytesText(size))"]
        if speed > 0 {
            parts.append(bytesText(Int64(speed)) + "/s")
            parts.append(etaText(Double(size - done) / speed))
        }
        info.stringValue = parts.joined(separator: " · ")
    }
}

// ---------- icona e menu ----------

enum Mode { case sleep, run, alert }

final class MenuController: NSObject, NSApplicationDelegate {
    var item: NSStatusItem!
    var images: [String: NSImage] = [:]
    var frames: [NSImage] = []
    var frameIndex = 0
    var mode: Mode? = nil
    var timer: Timer? = nil
    let poller = Poller()

    let menu = NSMenu()
    let titleItem = NSMenuItem(title: "MIMUS", action: nil, keyEquivalent: "")
    var detailItems: [NSMenuItem] = []
    var openItem: NSMenuItem!
    var refreshItem: NSMenuItem!
    var lastLines: [String] = []
    var lastLog = ""
    var transferItems: [NSMenuItem] = []
    var transferRows: [String: TransferRow] = [:]
    var transferNames: [String] = []
    let maxRows = 5
    var lastProgressLog = ""

    var mountPath = NSHomeDirectory() + "/MIMUS"
    var downSince: Date? = Date()
    var everUp = false
    var unmountedSince: Date? = nil
    var refreshRequested: Date? = nil

    func applicationDidFinishLaunching(_ notification: Notification) {
        for n in allFrames { images[n] = loadFrame(n) }
        item = NSStatusBar.system.statusItem(withLength: NSStatusItem.variableLength)
        item.autosaveName = "MIMUS"
        if let b = item.button {
            b.imagePosition = .imageOnly
            b.setAccessibilityLabel("MIMUS")
        }
        buildMenu()
        item.menu = menu
        setMode(.sleep)
        setTitle("avvio…", [])

        poller.receiver = self
        let t = Thread(target: poller, selector: #selector(Poller.loop), object: nil)
        t.qualityOfService = .utility
        t.start()
    }

    func buildMenu() {
        menu.autoenablesItems = false
        titleItem.isEnabled = false
        menu.addItem(titleItem)
        menu.addItem(NSMenuItem.separator())
        openItem = add("Apri cartella", #selector(openFolder))
        refreshItem = add("Aggiorna cartelle", #selector(refreshFolders))
        menu.addItem(NSMenuItem.separator())
        _ = add("Apri MIMUS…", #selector(openApp))
        _ = add("Guida", #selector(openGuide))
        menu.addItem(NSMenuItem.separator())
        _ = add("Nascondi l'icona (torna riaprendo MIMUS)", #selector(hideIcon))
    }

    func add(_ title: String, _ action: Selector) -> NSMenuItem {
        let mi = NSMenuItem(title: title, action: action, keyEquivalent: "")
        mi.target = self
        menu.addItem(mi)
        return mi
    }

    // ---- animazione ----

    func setMode(_ m: Mode) {
        if mode == m { return }
        mode = m
        timer?.invalidate()
        timer = nil
        let names: [String]
        let interval: TimeInterval
        switch m {
        case .run: names = runFrames; interval = 0.13
        case .sleep: names = sleepFrames; interval = 0.8
        case .alert: names = ["alert"]; interval = 0
        }
        frames = names.compactMap { images[$0] }
        frameIndex = 0
        showFrame()
        if frames.count > 1 {
            let t = Timer(timeInterval: interval, target: self, selector: #selector(tick), userInfo: nil, repeats: true)
            t.tolerance = interval / 5
            RunLoop.main.add(t, forMode: .common)
            timer = t
        }
    }

    @objc func tick() {
        guard !frames.isEmpty else { return }
        frameIndex = (frameIndex + 1) % frames.count
        showFrame()
    }

    func showFrame() {
        guard let b = item.button else { return }
        if frames.isEmpty { // immagini mancanti: almeno il nome
            b.image = nil
            b.title = mode == .alert ? "MIMUS !" : "MIMUS"
            return
        }
        b.title = ""
        b.image = frames[frameIndex % frames.count]
    }

    // ---- testo del menu ----

    func setTitle(_ title: String, _ lines: [String]) {
        titleItem.title = "MIMUS – " + title
        item.button?.toolTip = ([titleItem.title] + lines).joined(separator: "\n")
        if debug {
            let m = mode.map { "\($0)" } ?? "-"
            let line = "stato: \(m) | \(title) | \(lines.joined(separator: " / "))\n"
            if line != lastLog {
                lastLog = line
                FileHandle.standardError.write(line.data(using: .utf8) ?? Data())
            }
        }
        if lines == lastLines { return }
        lastLines = lines
        for d in detailItems { menu.removeItem(d) }
        detailItems = lines.map { l in
            let mi = NSMenuItem(title: l, action: nil, keyEquivalent: "")
            mi.isEnabled = false
            return mi
        }
        for (i, d) in detailItems.enumerated() { menu.insertItem(d, at: 1 + i) }
        transferNames = [] // le righe dei file vanno rimesse dopo queste
    }

    // ---- file in scaricamento o caricamento: una riga con la barra ----

    func showTransfers(_ list: [[String: Any]]) {
        let shown = Array(list.prefix(maxRows))
        let names = shown.map { ($0["name"] as? String) ?? "" } + (list.count > maxRows ? ["+\(list.count - maxRows)"] : [])
        if names != transferNames {
            transferNames = names
            for mi in transferItems {
                mi.view = nil
                menu.removeItem(mi)
            }
            transferItems = []
            var rows: [String: TransferRow] = [:]
            var at = 1 + detailItems.count
            for t in shown {
                let key = (t["name"] as? String) ?? ""
                let row = transferRows[key] ?? TransferRow()
                rows[key] = row
                let mi = NSMenuItem()
                mi.view = row
                menu.insertItem(mi, at: at)
                transferItems.append(mi)
                at += 1
            }
            if list.count > maxRows {
                let mi = NSMenuItem(title: "e altri \(list.count - maxRows) file", action: nil, keyEquivalent: "")
                mi.isEnabled = false
                menu.insertItem(mi, at: at)
                transferItems.append(mi)
            }
            transferRows = rows
        }
        for t in shown {
            if let key = t["name"] as? String { transferRows[key]?.update(t) }
        }
        if debug {
            let line = "avanzamento: " + shown.compactMap { t in
                (t["name"] as? String).map { "\($0) " + (transferRows[$0]?.info.stringValue ?? "") }
            }.joined(separator: " | ") + "\n"
            if line != lastProgressLog {
                lastProgressLog = line
                FileHandle.standardError.write(line.data(using: .utf8) ?? Data())
            }
        }
    }

    // ---- stato ----

    @objc func apply(_ obj: Any?) {
        guard let s = obj as? Snapshot else { return }
        defer { showTransfers(s.rcUp ? s.transfers : []) }
        let now = Date()
        let settings = readSettings()
        if let mp = settings?["mount_path"] as? String, !mp.isEmpty { mountPath = mp }
        let configured = !((settings?["host"] as? String) ?? "").isEmpty

        if s.rcUp {
            downSince = nil
            everUp = true
        } else if downSince == nil {
            downSince = now
        }
        let mounted = s.mounts.contains(mountPath) || s.mounts.contains((mountPath as NSString).resolvingSymlinksInPath)
        if s.rcUp && !mounted {
            if unmountedSince == nil { unmountedSince = now }
        } else {
            unmountedSince = nil
        }
        openItem.isEnabled = mounted
        refreshItem.isEnabled = false

        if !configured {
            setMode(.alert)
            setTitle("da configurare", ["Apri MIMUS per collegare la cartella remota."])
            return
        }
        if !s.rcUp {
            // all'accensione il servizio impiega qualche secondo a partire
            let grace: TimeInterval = everUp ? 8 : 45
            if let since = downSince, now.timeIntervalSince(since) >= grace {
                setMode(.alert)
                setTitle("collegamento fermo", ["Apri MIMUS e scegli «Avvia collegamento»."])
            } else {
                setMode(.sleep)
                setTitle("avvio del collegamento…", [])
            }
            return
        }

        let st = s.status
        let online = flag(st["online"])
        let journal = num(st["journal"])
        let parked = num(st["parked"])
        let problem = (st["problem"] as? String) ?? ""
        let dc = (s.vfs["diskCache"] as? [String: Any]) ?? [:]
        let uploading = num(dc["uploadsInProgress"])
        let toUpload = uploading + num(dc["uploadsQueued"])
        let errored = num(dc["erroredFiles"])
        let outOfSpace = flag(dc["outOfSpace"])
        let transferring = (s.core["transferring"] as? [Any])?.count ?? 0
        let downloading = max(0, transferring - uploading)
        let replaying = flag(st["replaying"])
        let walking = flag(st["walking"])
        // il lavoro di aggiornamento appena chiesto compare al giro successivo
        let refreshing = s.refreshing || (refreshRequested.map { now.timeIntervalSince($0) < 5 } ?? false)
        refreshItem.isEnabled = online

        var lines: [String] = []
        func pending() {
            if toUpload > 0 { lines.append(plural(toUpload, "file da caricare", "file da caricare") + " sul server remoto") }
            if journal > 0 { lines.append(plural(journal, "modifica fatta offline", "modifiche fatte offline") + " da applicare") }
        }

        if !problem.isEmpty || errored > 0 || outOfSpace || flag(st["authFailed"]) {
            setMode(.alert)
            var t = problem
            if t.isEmpty { t = "Spazio quasi esaurito sul disco" }
            if parked > 0 { lines.append(plural(parked, "modifica non applicata", "modifiche non applicate") + ": vedi «Ultimi errori» in MIMUS") }
            pending()
            lines.append("Apri MIMUS per i dettagli.")
            setTitle(lowerFirst(t), lines)
            return
        }
        if let since = unmountedSince, now.timeIntervalSince(since) > 30 {
            setMode(.alert)
            setTitle("cartella non collegata nel Finder", ["Apri MIMUS e scegli «Riavvia collegamento»."])
            return
        }
        if online && (toUpload > 0 || transferring > 0 || journal > 0 || replaying || walking || refreshing) {
            setMode(.run)
            if downloading > 0 { lines.append("Scarico " + plural(downloading, "file", "file") + " dal server remoto") }
            pending()
            if walking || refreshing { lines.append("Aggiorno l'elenco delle cartelle") }
            setTitle("sincronizzazione in corso", lines)
            return
        }
        setMode(.sleep)
        if !online {
            lines.append("Server remoto non raggiungibile: lavori sui file già sul Mac")
            pending()
            if toUpload + journal > 0 { lines.append("Verranno caricati al ritorno in rete") }
            setTitle("offline", lines)
            return
        }
        setTitle("tutto sincronizzato", [])
    }

    func plural(_ n: Int, _ one: String, _ many: String) -> String {
        return n == 1 ? "1 " + one : "\(n) " + many
    }

    func lowerFirst(_ s: String) -> String {
        guard let f = s.first else { return s }
        return String(f).lowercased() + s.dropFirst()
    }

    // ---- azioni ----

    // l'app MIMUS che contiene questa: MIMUS.app/Contents/Library/LoginItems/MIMUS Menu.app
    func mainApp() -> URL? {
        var u = Bundle.main.bundleURL
        for _ in 0..<4 { u = u.deletingLastPathComponent() }
        if u.pathExtension == "app" && FileManager.default.fileExists(atPath: u.path) { return u }
        return NSWorkspace.shared.urlForApplication(withBundleIdentifier: "app.mimus")
    }

    @objc func openFolder() {
        _ = NSWorkspace.shared.open(URL(fileURLWithPath: mountPath, isDirectory: true))
    }

    @objc func refreshFolders() {
        refreshRequested = Date()
        setMode(.run)
        let p = poller
        DispatchQueue.global(qos: .utility).async { p.startRefresh() }
    }

    @objc func openApp() {
        if let u = mainApp() { _ = NSWorkspace.shared.open(u) }
    }

    @objc func openGuide() {
        if let u = mainApp() {
            _ = NSWorkspace.shared.open(u.appendingPathComponent("Contents/Resources/Guida.html"))
        }
    }

    @objc func hideIcon() {
        NSApp.terminate(nil) // uscita normale: launchd non la riavvia
    }
}

// ---------- avvio ----------

if CommandLine.arguments.contains("--selftest") {
    let missing = allFrames.filter { loadFrame($0) == nil }
    let id = Bundle.main.bundleIdentifier ?? "?"
    if !missing.isEmpty || id != "app.mimus.menu" {
        print("MIMUS Menu: immagini mancanti \(missing), bundle \(id)")
        exit(1)
    }
    print("MIMUS Menu: ok (\(allFrames.count) immagini, \(id))")
    exit(0)
}

let me = getpid()
let others = NSRunningApplication.runningApplications(withBundleIdentifier: "app.mimus.menu").filter { $0.processIdentifier != me }
if !others.isEmpty { exit(0) } // già in esecuzione

let app = NSApplication.shared
let controller = MenuController()
app.delegate = controller
app.setActivationPolicy(.accessory)
app.run()
