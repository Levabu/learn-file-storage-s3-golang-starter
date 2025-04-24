package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"math"
	"os/exec"
)

type FfprobeOut struct {
	Streams []struct {
		Width  int `json:"width"`
		Height int `json:"height"`
	} `json:"streams"`
}

func getVideoAspectRatio(filePath string) (string, error) {
	cmd := exec.Command("ffprobe", "-v", "error", "-print_format", "json", "-show_streams", filePath)
	cmdOut := bytes.Buffer{}
	cmd.Stdout = &cmdOut
	err := cmd.Run()
	if err != nil {
		return "", err
	}

	var res FfprobeOut
	err = json.Unmarshal(cmdOut.Bytes(), &res)
	if err != nil {
		return "", err
	}

	if len(res.Streams) == 0 {
		return "", errors.New("no streams found")
	}
	stream := res.Streams[0]
	if stream.Width == 0 || stream.Height == 0 {
		return "", errors.New("no width or height found in stream")
	}
	return getAspectRatio(stream.Width, stream.Height), nil
}

func getAspectRatio(width, height int) string {
	ratio := float64(width) / float64(height)
	if math.Abs(ratio - 9.0 / 16.0) < 0.001 {
		return "9:16"
	}
	if math.Abs(ratio - 16.0 / 9.0) < 0.001 {
		return "16:9"
	}
	return "other"
}

func processVideoForFastStart(filePath string) (string, error) {
	newFilePath := filePath + ".processing"
	cmd := exec.Command("ffmpeg", "-i", filePath, "-c", "copy", "-movflags", "faststart", "-f", "mp4", newFilePath)
	err := cmd.Run()
	if err != nil {
		return "", err
	}
	return newFilePath, nil
}