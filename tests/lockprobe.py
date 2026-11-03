# Blocchi sui file come li usano InDesign, Office e gli altri programmi che
# impediscono di aprire due volte lo stesso documento. Stampa una riga:
#   flock ok, fcntl ok, exlock ok, secondo rifiutato   (tutto giusto)
#   python3 tests/lockprobe.py <file>
import errno, fcntl, os, signal, sys

def alarm(*_):
    raise TimeoutError("nessuna risposta in 20 s")

signal.signal(signal.SIGALRM, alarm)
p = sys.argv[1]
res = []

def attempt(name, fn):
    signal.alarm(20)
    try:
        extra = fn()
        res.append(name + " ok" + (", " + extra if extra else ""))
    except TimeoutError as e:
        res.append(name + " " + str(e))
    except OSError as e:
        res.append(name + " " + os.strerror(e.errno))
    except Exception as e:
        res.append(name + " " + str(e))
    finally:
        signal.alarm(0)

def do_flock():
    fd = os.open(p, os.O_RDWR)
    try:
        fcntl.flock(fd, fcntl.LOCK_EX | fcntl.LOCK_NB)
        fcntl.flock(fd, fcntl.LOCK_UN)
    finally:
        os.close(fd)

def do_fcntl():
    fd = os.open(p, os.O_RDWR)
    try:
        fcntl.lockf(fd, fcntl.LOCK_EX | fcntl.LOCK_NB)
        fcntl.lockf(fd, fcntl.LOCK_UN)
    finally:
        os.close(fd)

def do_exlock():
    ex = getattr(os, "O_EXLOCK", None)  # solo macOS
    if ex is None:
        return "non disponibile"
    fd = os.open(p, os.O_RDWR | ex | os.O_NONBLOCK)
    try:
        try:
            fd2 = os.open(p, os.O_RDWR | ex | os.O_NONBLOCK)
            os.close(fd2)
            return "secondo NON rifiutato"
        except OSError as e:
            return "secondo rifiutato" if e.errno in (errno.EAGAIN, errno.EWOULDBLOCK) else "secondo " + os.strerror(e.errno)
    finally:
        os.close(fd)

attempt("flock", do_flock)
attempt("fcntl", do_fcntl)
attempt("exlock", do_exlock)
print(", ".join(res))
