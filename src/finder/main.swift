// MIMUS Finder: estensione del Finder per la cartella collegata.
//
// Pallini sui file:
//   cerchio grigio      solo sul server remoto (si scarica quando lo apri)
//   pallino verde       su questo Mac (si apre anche offline)
//   verde con spunta    sempre su questo Mac (non viene mai tolto)
//   arancione con ↑     modificato qui, da caricare sul server remoto
// Menu (tasto destro): Scarica ora su questo Mac, Mantieni sempre su questo
// Mac / Non mantenere più, Togli da questo Mac (resta sul server).
//
// Parla con il servizio di MIMUS (rclone, rc su 127.0.0.1:5572):
// vfs/mimus-states, vfs/mimus-download, vfs/mimus-pin, vfs/mimus-evict.
// Gira nella sandbox: legge solo le impostazioni di MIMUS (posizione della
// cartella collegata) e si collega solo al servizio locale.
import Cocoa
import FinderSync
import os

let rcBase = "http://127.0.0.1:5572/"
let logger = Logger(subsystem: "app.mimus.finder", category: "finder")

// messaggi: registro di sistema (log show --predicate 'subsystem ==
// "app.mimus.finder"') e un file nel contenitore dell'estensione
// (~/Library/Containers/app.mimus.finder/Data/tmp/mimus-finder.log)
func dlog(_ msg: String) {
    logger.notice("\(msg, privacy: .public)")
    let url = FileManager.default.temporaryDirectory.appendingPathComponent("mimus-finder.log")
    let line = "\(Date()) \(msg)\n"
    if let size = (try? FileManager.default.attributesOfItem(atPath: url.path)[.size] as? NSNumber)??.intValue, size > 200_000 {
        try? FileManager.default.removeItem(at: url)
    }
    if let h = try? FileHandle(forWritingTo: url) {
        h.seekToEndOfFile()
        h.write(line.data(using: .utf8) ?? Data())
        h.closeFile()
    } else {
        try? line.data(using: .utf8)?.write(to: url)
    }
}

// cartella Inizio vera (nella sandbox NSHomeDirectory è il contenitore)
func realHome() -> String {
    if let pw = getpwuid(getuid()), let dir = pw.pointee.pw_dir {
        return String(cString: dir)
    }
    return NSHomeDirectory()
}

func mountPath() -> String {
    let file = realHome() + "/Library/Application Support/MIMUS/settings.json"
    if let data = FileManager.default.contents(atPath: file),
       let obj = try? JSONSerialization.jsonObject(with: data, options: []),
       let dict = obj as? [String: Any],
       let mp = dict["mount_path"] as? String, !mp.isEmpty {
        return mp
    }
    return realHome() + "/MIMUS"
}

final class Box: @unchecked Sendable {
    var value: [String: Any]? = nil
}

let session: URLSession = {
    let cfg = URLSessionConfiguration.ephemeral
    cfg.timeoutIntervalForRequest = 10
    cfg.connectionProxyDictionary = [:]
    return URLSession(configuration: cfg)
}()

// chiamata rc sincrona (da non usare sul thread principale)
func rcCall(_ path: String, _ params: [String: Any]) -> [String: Any]? {
    guard let url = URL(string: rcBase + path) else { return nil }
    var req = URLRequest(url: url)
    req.httpMethod = "POST"
    req.setValue("application/json", forHTTPHeaderField: "Content-Type")
    req.httpBody = try? JSONSerialization.data(withJSONObject: params, options: [])
    let box = Box()
    let done = DispatchSemaphore(value: 0)
    let task = session.dataTask(with: req) { data, _, _ in
        if let data = data,
           let obj = try? JSONSerialization.jsonObject(with: data, options: []) as? [String: Any] {
            box.value = obj
        }
        done.signal()
    }
    task.resume()
    if done.wait(timeout: .now() + 12) == .timedOut {
        task.cancel()
        return nil
    }
    return box.value
}

// ---------- pallini ----------

func badgeImage(fill: NSColor?, ring: NSColor?, glyph: String?) -> NSImage {
    return NSImage(size: NSSize(width: 64, height: 64), flipped: false) { rect in
        let r = rect.insetBy(dx: 6, dy: 6)
        // bordo bianco: visibile su qualsiasi icona
        NSColor.white.setFill()
        NSBezierPath(ovalIn: rect.insetBy(dx: 2, dy: 2)).fill()
        if let fill = fill {
            fill.setFill()
            NSBezierPath(ovalIn: r).fill()
        }
        if let ring = ring {
            ring.setStroke()
            let p = NSBezierPath(ovalIn: r.insetBy(dx: 5, dy: 5))
            p.lineWidth = 9
            p.stroke()
        }
        NSColor.white.setStroke()
        let g = NSBezierPath()
        g.lineWidth = 8
        g.lineCapStyle = .round
        g.lineJoinStyle = .round
        switch glyph {
        case "check":
            g.move(to: NSPoint(x: 19, y: 33))
            g.line(to: NSPoint(x: 28, y: 23))
            g.line(to: NSPoint(x: 45, y: 42))
            g.stroke()
        case "up":
            g.move(to: NSPoint(x: 32, y: 18))
            g.line(to: NSPoint(x: 32, y: 46))
            g.move(to: NSPoint(x: 21, y: 35))
            g.line(to: NSPoint(x: 32, y: 46))
            g.line(to: NSPoint(x: 43, y: 35))
            g.stroke()
        default:
            break
        }
        return true
    }
}

// ---------- estensione ----------

@objc(FinderSync)
final class FinderSync: FIFinderSync {
    let queue = DispatchQueue(label: "app.mimus.finder")
    var root = ""                         // cartella collegata
    var observed = Set<String>()          // cartelle mostrate (relative)
    var pending = Set<String>()           // cartelle da rileggere subito
    var shown: [String: String] = [:]     // percorso -> pallino mostrato
    var pinned: [String: Bool] = [:]      // percorso -> mantenuto sempre
    var dirPinned: [String: Bool] = [:]   // cartella -> dentro una cartella mantenuta
    var timer: DispatchSourceTimer?
    var ticks = 0

    override init() {
        super.init()
        let c = FIFinderSyncController.default()
        c.setBadgeImage(badgeImage(fill: nil, ring: .systemGray, glyph: nil), label: "Solo sul server remoto", forBadgeIdentifier: "remote")
        c.setBadgeImage(badgeImage(fill: .systemGreen, ring: nil, glyph: nil), label: "Su questo Mac", forBadgeIdentifier: "local")
        c.setBadgeImage(badgeImage(fill: .systemGreen, ring: nil, glyph: "check"), label: "Sempre su questo Mac", forBadgeIdentifier: "pinned")
        c.setBadgeImage(badgeImage(fill: .systemOrange, ring: nil, glyph: "up"), label: "Da caricare sul server remoto", forBadgeIdentifier: "upload")
        queue.sync { self.updateRoot() }
        let t = DispatchSource.makeTimerSource(queue: queue)
        t.schedule(deadline: .now() + 1, repeating: 0.5)
        t.setEventHandler { [weak self] in self?.tick() }
        t.resume()
        timer = t
        dlog("avviata, cartella collegata \(root)")
    }

    // su queue
    func updateRoot() {
        let mp = (mountPath() as NSString).standardizingPath
        if mp == root { return }
        root = mp
        shown.removeAll()
        dlog("cartella collegata \(mp)")
        let url = URL(fileURLWithPath: mp, isDirectory: true)
        DispatchQueue.main.async {
            FIFinderSyncController.default().directoryURLs = [url]
        }
    }

    // percorso relativo alla cartella collegata ("" = la cartella stessa)
    func rel(_ url: URL) -> String? {
        let p = (url.path as NSString).standardizingPath
        if p == root { return "" }
        if p.hasPrefix(root + "/") { return String(p.dropFirst(root.count + 1)) }
        return nil
    }

    func url(_ rel: String) -> URL {
        return URL(fileURLWithPath: rel.isEmpty ? root : root + "/" + rel)
    }

    // ogni mezzo secondo: le cartelle appena chieste; ogni 3 secondi tutte
    // quelle mostrate; ogni 10 secondi la posizione della cartella collegata
    func tick() {
        ticks += 1
        if ticks % 20 == 0 { updateRoot() }
        var dirs = pending
        pending.removeAll()
        if ticks % 6 == 0 { dirs.formUnion(observed) }
        for d in dirs { refresh(d) }
    }

    func refresh(_ dir: String) {
        guard let out = rcCall("vfs/mimus-states", ["dir": dir]),
              let entries = out["entries"] as? [[String: Any]] else { return }
        dirPinned[dir] = (out["pinned"] as? Bool) ?? false
        var changes: [(URL, String)] = []
        for e in entries {
            guard let name = e["name"] as? String else { continue }
            let state = (e["state"] as? String) ?? "remote"
            let pin = (e["pinned"] as? Bool) ?? false
            let r = dir.isEmpty ? name : dir + "/" + name
            var badge: String
            switch state {
            case "folder": badge = pin ? "pinned" : ""
            case "upload": badge = "upload"
            case "local": badge = pin ? "pinned" : "local"
            default: badge = "remote"
            }
            if name.hasPrefix(".") { badge = "" }
            pinned[r] = pin
            if shown[r] != badge {
                shown[r] = badge
                changes.append((url(r), badge))
            }
        }
        if !changes.isEmpty {
            let summary = changes.map { "\($0.0.lastPathComponent)=\($0.1.isEmpty ? "-" : $0.1)" }.joined(separator: " ")
            dlog("«\(dir.isEmpty ? "/" : dir)» \(summary)")
            DispatchQueue.main.async {
                let c = FIFinderSyncController.default()
                for (u, b) in changes { c.setBadgeIdentifier(b, for: u) }
            }
        }
    }

    // ---- il Finder mostra una cartella ----

    override func beginObservingDirectory(at url: URL) {
        queue.async {
            guard let r = self.rel(url) else { return }
            self.observed.insert(r)
            self.pending.insert(r)
        }
    }

    override func endObservingDirectory(at url: URL) {
        queue.async {
            guard let r = self.rel(url) else { return }
            self.observed.remove(r)
        }
    }

    override func requestBadgeIdentifier(for url: URL) {
        queue.async {
            guard let r = self.rel(url), !r.isEmpty else { return }
            if let b = self.shown[r] {
                DispatchQueue.main.async { FIFinderSyncController.default().setBadgeIdentifier(b, for: url) }
            }
            self.pending.insert((r as NSString).deletingLastPathComponent)
        }
    }

    // ---- menu (tasto destro) ----

    func selection() -> [String] {
        let urls = FIFinderSyncController.default().selectedItemURLs() ?? []
        return queue.sync { urls.compactMap { self.rel($0) } }
    }

    override func menu(for menuKind: FIMenuKind) -> NSMenu? {
        guard menuKind == .contextualMenuForItems else { return nil }
        let sel = selection()
        if sel.isEmpty { return nil }
        let (allPinned, insidePinned) = queue.sync { () -> (Bool, Bool) in
            let all = sel.allSatisfy { self.pinned[$0] ?? false }
            let inside = sel.contains { self.dirPinned[($0 as NSString).deletingLastPathComponent] ?? false }
            return (all, inside)
        }
        let m = NSMenu(title: "MIMUS")
        m.addItem(withTitle: "Scarica ora su questo Mac", action: #selector(downloadNow(_:)), keyEquivalent: "")
        if allPinned {
            let it = m.addItem(withTitle: "Non mantenere più sempre su questo Mac", action: #selector(unpin(_:)), keyEquivalent: "")
            it.isEnabled = !insidePinned
        } else {
            m.addItem(withTitle: "Mantieni sempre su questo Mac", action: #selector(pin(_:)), keyEquivalent: "")
        }
        let ev = m.addItem(withTitle: insidePinned ? "Togli da questo Mac (la cartella che lo contiene è mantenuta)" : "Togli da questo Mac (resta sul server)", action: #selector(evict(_:)), keyEquivalent: "")
        ev.isEnabled = !insidePinned
        return m
    }

    func run(_ path: String, _ params: [String: Any], _ sel: [String]) {
        queue.async {
            let r = rcCall(path, params)
            if let err = r?["error"] as? String {
                dlog("\(path) non riuscito: \(err)")
            }
            for s in sel { self.pending.insert((s as NSString).deletingLastPathComponent) }
        }
    }

    @IBAction func downloadNow(_ sender: AnyObject?) {
        let sel = selection()
        run("vfs/mimus-download", ["paths": sel, "_async": true], sel)
    }

    @IBAction func pin(_ sender: AnyObject?) {
        let sel = selection()
        run("vfs/mimus-pin", ["paths": sel, "on": true], sel)
    }

    @IBAction func unpin(_ sender: AnyObject?) {
        let sel = selection()
        run("vfs/mimus-pin", ["paths": sel, "on": false], sel)
    }

    @IBAction func evict(_ sender: AnyObject?) {
        let sel = selection()
        run("vfs/mimus-evict", ["paths": sel, "_async": true], sel)
    }
}
