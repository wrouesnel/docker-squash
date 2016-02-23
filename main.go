package main

import (
	"flag"
	"fmt"
	"io/ioutil"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"

	"github.com/wrouesnel/docker-squash/export"
	"github.com/wrouesnel/go.log"
)

var (
	buildVersion string
	signals      chan os.Signal
	wg           sync.WaitGroup
)

func shutdown(tempdir string) {
	defer wg.Done()
	<-signals
	log.Debugf("Removing tempdir %s\n", tempdir)
	err := os.RemoveAll(tempdir)
	if err != nil {
		log.Fatalln(err)
	}

}

func main() {
	var from, input, output, tempdir, tag string
	var keepTemp, version bool
	flag.StringVar(&input, "i", "", "Read from a tar archive file, instead of STDIN")
	flag.StringVar(&output, "o", "", "Write to a file, instead of STDOUT")
	flag.StringVar(&tag, "t", "", "Repository name and tag for new image")
	flag.StringVar(&from, "from", "", "Squash from layer ID (default: first FROM layer)")
	flag.BoolVar(&keepTemp, "keepTemp", false, "Keep temp dir when done. (Useful for debugging)")
	flag.BoolVar(&version, "v", false, "Print version information and quit")

	flag.Usage = func() {
		fmt.Printf("\nUsage: docker-squash [options]\n\n")
		fmt.Printf("Squashes the layers of a tar archive on STDIN and streams it to STDOUT\n\n")
		fmt.Printf("Options:\n")
		flag.PrintDefaults()
	}
	flag.Parse()

	if version {
		fmt.Println(buildVersion)
		return
	}

	var err error
	tempdir, err = ioutil.TempDir("", "docker-squash")
	if err != nil {
		log.Fatalln(err)
	}

	if tag != "" && strings.Contains(tag, ":") {
		parts := strings.Split(tag, ":")
		if parts[0] == "" || parts[1] == "" {
			log.Fatalf("bad tag format: %s\n", tag)
		}
	}

	signals = make(chan os.Signal, 1)

	if !keepTemp {
		wg.Add(1)
		signal.Notify(signals, os.Interrupt, os.Kill, syscall.SIGTERM)
		go shutdown(tempdir)
	}

	dockerExport, err := export.LoadExport(input, tempdir)
	if err != nil {
		log.Fatalln(err)
	}

	// Export may have multiple branches with the same parent.
	// We can't handle that currently so abort.
	for _, v := range dockerExport.Repositories {
		commits := map[string]string{}
		for tag, commit := range *v {
			commits[commit] = tag
		}
		if len(commits) > 1 {
			log.Fatalln("This image is a full repository export w/ multiple images in it.  " +
				"You need to generate the export from a specific image ID or tag.")
		}

	}

	start := dockerExport.FirstSquash()
	// Can't find a previously squashed layer, use first FROM
	if start == nil {
		start = dockerExport.FirstFrom()
	}
	// Can't find a FROM, default to root
	if start == nil {
		start = dockerExport.Root()
	}

	if from != "" {
		if from == "root" {
			start = dockerExport.Root()
		} else {
			start, err = dockerExport.GetById(from)
			if err != nil {
				log.Fatalln(err)
			}
		}
	}

	if start == nil {
		log.Fatalf("no layer matching %s\n", from)
		return
	}

	// extract each "layer.tar" to "layer" dir
	err = dockerExport.ExtractLayers()
	if err != nil {
		log.Fatalln(err)
		return
	}

	// insert a new layer after our squash point
	newEntry, err := dockerExport.InsertLayer(start.LayerConfig.Id)
	if err != nil {
		log.Fatalln(err)
		return
	}

	log.Debugf("Inserted new layer %s after %s\n", newEntry.LayerConfig.Id[0:12],
		newEntry.LayerConfig.Parent[0:12])

	//if verbose {
	//	e := export.Root()
	//	for {
	//		if e == nil {
	//			break
	//		}
	//		cmd := strings.Join(e.LayerConfig.ContainerConfig().Cmd, " ")
	//		if len(cmd) > 60 {
	//			cmd = cmd[:60]
	//		}
	//
	//		if e.LayerConfig.Id == newEntry.LayerConfig.Id {
	//			log.Debugf("  -> %s %s\n", e.LayerConfig.Id[0:12], cmd)
	//		} else {
	//			log.Debugf("  -  %s %s\n", e.LayerConfig.Id[0:12], cmd)
	//		}
	//		e = export.ChildOf(e.LayerConfig.Id)
	//	}
	//}

	// squash all later layers into our new layer
	err = dockerExport.SquashLayers(newEntry, newEntry)
	if err != nil {
		log.Fatalln(err)
		return
	}

	log.Debugf("Tarring up squashed layer %s\n", newEntry.LayerConfig.Id[:12])
	// create a layer.tar from our squashed layer
	err = newEntry.TarLayer()
	if err != nil {
		log.Fatalln(err)
	}

	log.Debugf("Removing extracted layers\n")
	// remove our expanded "layer" dirs
	err = dockerExport.RemoveExtractedLayers()
	if err != nil {
		log.Fatalln(err)
	}

	if tag != "" {
		tagPart := "latest"
		repoPart := tag
		parts := strings.Split(tag, ":")
		if len(parts) > 1 {
			repoPart = parts[0]
			tagPart = parts[1]
		}
		tagInfo := export.TagInfo{}
		layer := dockerExport.LastChild()

		tagInfo[tagPart] = layer.LayerConfig.Id
		dockerExport.Repositories[repoPart] = &tagInfo

		log.Debugf("Tagging %s as %s:%s\n", layer.LayerConfig.Id[0:12], repoPart, tagPart)
		err := dockerExport.WriteRepositoriesJson()
		if err != nil {
			log.Fatalln(err)
		}
	}

	ow := os.Stdout
	if output != "" {
		var err error
		ow, err = os.Create(output)
		if err != nil {
			log.Fatalln(err)
		}
		log.Debugf("Tarring new image to %s\n", output)
	} else {
		log.Debugf("Tarring new image to STDOUT\n")
	}
	// bundle up the new image
	err = dockerExport.TarLayers(ow)
	if err != nil {
		log.Fatalln(err)
	}

	log.Debugln("Done. New image created.")
	// print our new history
	dockerExport.PrintHistory()

	signals <- os.Interrupt
	wg.Wait()
}
