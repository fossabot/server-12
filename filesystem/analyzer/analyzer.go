package analyzer

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/dhowden/tag"
	"github.com/dhowden/tag/mbz"
	"github.com/meteorae/meteorae-server/database/models"
	"github.com/rs/zerolog/log"
	"gopkg.in/vansante/go-ffprobe.v2"
	"gorm.io/gorm"
)

var ffprobeProcessTimeout = 5 * time.Second

func AnalyzeAudio(mediaPart models.MediaPart, database *gorm.DB) error {
	log.Debug().Msgf("Analyzing %s", mediaPart.FilePath)

	err := getFfprobeData(mediaPart, database)
	if err != nil {
		return fmt.Errorf("could not get ffprobe data: %w", err)
	}

	mediaFile, err := os.Open(mediaPart.FilePath)
	if err != nil {
		return fmt.Errorf("could not open file: %w", err)
	}
	defer mediaFile.Close()

	mediaTags, err := tag.ReadFrom(mediaFile)
	if err != nil {
		return fmt.Errorf("could not read tags: %w", err)
	}

	musicBrainzTags := mbz.Extract(mediaTags)

	fingerprint := musicBrainzTags.Get(mbz.AcoustFingerprint)
	if fingerprint != "" {
		mediaPart.AcoustID = fingerprint
		database.Save(&mediaPart)
	}

	return nil
}

func AnalyzeVideo(mediaPart models.MediaPart, database *gorm.DB) error {
	log.Debug().Msgf("Analyzing %s", mediaPart.FilePath)

	err := getFfprobeData(mediaPart, database)
	if err != nil {
		return fmt.Errorf("could not get ffprobe data: %w", err)
	}

	return nil
}

func getFfprobeData(mediaPart models.MediaPart, database *gorm.DB) error {
	ctx, cancelFn := context.WithTimeout(context.Background(), ffprobeProcessTimeout)
	defer cancelFn()

	data, err := ffprobe.ProbeURL(ctx, mediaPart.FilePath)
	if err != nil {
		return fmt.Errorf("could not probe file: %w", err)
	}

	for _, stream := range data.Streams {
		streamInfo := models.MediaStreamInfo{
			CodecName:          stream.CodecName,
			CodecLongName:      stream.CodecLongName,
			CodecType:          stream.CodecType,
			CodecTimeBase:      stream.CodecTimeBase,
			CodecTag:           stream.CodecTagString,
			RFrameRate:         stream.RFrameRate,
			AvgFrameRate:       stream.AvgFrameRate,
			TimeBase:           stream.TimeBase,
			StartPts:           stream.StartPts,
			StartTime:          stream.StartTime,
			DurationTS:         stream.DurationTs,
			Duration:           stream.Duration,
			BitRate:            stream.BitRate,
			BitsPerRawSample:   stream.BitsPerRawSample,
			NbFrames:           stream.NbFrames,
			Disposition:        stream.Disposition,
			Tags:               stream.Tags,
			Profile:            stream.Profile,
			Width:              stream.Width,
			Height:             stream.Height,
			HasBFrames:         stream.HasBFrames,
			SampleAspectRatio:  stream.SampleAspectRatio,
			DisplayAspectRatio: stream.DisplayAspectRatio,
			PixelFormat:        stream.PixFmt,
			Level:              stream.Level,
			ColorRange:         stream.ColorRange,
			ColorSpace:         stream.ColorSpace,
			SampleFmt:          stream.SampleFmt,
			SampleRate:         stream.SampleRate,
			Channels:           stream.Channels,
			ChannelsLayout:     stream.ChannelLayout,
			BitsPerSample:      stream.BitsPerSample,
		}

		jsonStreamInfo, err := json.Marshal(streamInfo)
		if err != nil {
			return fmt.Errorf("could not marshal stream info: %w", err)
		}

		var streamType models.StreamType

		switch stream.CodecType {
		case "video":
			streamType = models.VideoStream
		case "audio":
			streamType = models.AudioStream
		case "subtitle":
			streamType = models.SubtitleStream
		default:
			log.Debug().Msgf("Unhandled stream type: %s", stream.CodecType)

			return nil
		}

		mediaStream := models.MediaStream{
			Title:           stream.Tags.Title,
			StreamType:      streamType,
			Language:        stream.Tags.Language,
			Index:           stream.Index,
			MediaStreamInfo: jsonStreamInfo,
			MediaPartID:     mediaPart.ID,
		}

		result := database.Create(&mediaStream)
		if result.Error != nil {
			log.Err(result.Error).Msgf("Could not create stream %s", mediaPart.FilePath)

			return result.Error
		}
	}

	return nil
}
