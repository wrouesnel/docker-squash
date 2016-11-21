package export

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strconv"
	"time"
)

// Extracts a tar-file with the correct permissions to expand a docker
// image layer.
func ExtractTar(src, dest string) ([]byte, error) {
	// Docker likes to produce 0-length tar files in save files. These will
	// *fail* to extract with tar, but are valid (i.e. an empty directory should
	// result. Catch that situation here and return success.
	if st, err := os.Stat(src) ; st.Size() == 0 {
		return []byte{}, nil
	} else if err != nil {
		return []byte{}, err
	}

	cmd := exec.Command("tar", "--same-owner", "--xattrs", "--overwrite",
		"--preserve-permissions", "-xf", src, "-C", dest)
	return cmd.CombinedOutput()
}

func humanDuration(d time.Duration) string {
	if seconds := int(d.Seconds()); seconds < 1 {
		return "Less than a second"
	} else if seconds < 60 {
		return fmt.Sprintf("%d seconds", seconds)
	} else if minutes := int(d.Minutes()); minutes == 1 {
		return "About a minute"
	} else if minutes < 60 {
		return fmt.Sprintf("%d minutes", minutes)
	} else if hours := int(d.Hours()); hours == 1 {
		return "About an hour"
	} else if hours < 48 {
		return fmt.Sprintf("%d hours", hours)
	} else if hours < 24*7*2 {
		return fmt.Sprintf("%d days", hours/24)
	} else if hours < 24*30*3 {
		return fmt.Sprintf("%d weeks", hours/24/7)
	} else if hours < 24*365*2 {
		return fmt.Sprintf("%d months", hours/24/30)
	}
	return fmt.Sprintf("%f years", d.Hours()/24/365)
}

func truncateID(id string) string {
	shortLen := 12
	if len(id) < shortLen {
		shortLen = len(id)
	}
	return id[:shortLen]
}

func newID() (string, error) {
	for {
		id := make([]byte, 32)
		if _, err := io.ReadFull(rand.Reader, id); err != nil {
			return "", err
		}
		value := hex.EncodeToString(id)
		if _, err := strconv.ParseInt(truncateID(value), 10, 64); err == nil {
			continue
		}
		return value, nil
	}
}

func readJsonFile(path string, dest interface{}) error {
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	buf := bytes.NewBuffer([]byte{})

	_, err = buf.ReadFrom(f)
	if err != nil {
		f.Close()
		return err
	}

	err = json.Unmarshal(buf.Bytes(), dest)
	if err != nil {
		f.Close()
		return err
	}
	return nil
}
