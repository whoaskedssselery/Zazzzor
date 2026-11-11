package messages

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"golang.org/x/sync/singleflight"

	"github.com/whoaskedssselery/zazzzor/server/internal/linkpreview"
	"github.com/whoaskedssselery/zazzzor/server/internal/messages/store"
	"github.com/whoaskedssselery/zazzzor/server/internal/platform/db"
	"github.com/whoaskedssselery/zazzzor/server/internal/platform/snowflake"
)

const (
	previewWorkers       = 4
	previewQueueSize     = 128
	previewSuccessTTL    = 24 * time.Hour
	previewFailureTTL    = time.Hour
	previewSweepInterval = 24 * time.Hour
)

// PreviewFetcher builds a preview and stores any image through save.
type PreviewFetcher interface {
	Fetch(ctx context.Context, rawURL string, save linkpreview.SaveImage) (*linkpreview.Embed, linkpreview.Outcome)
}

type previews struct {
	fetcher PreviewFetcher
	jobs    chan previewJob
	shared  singleflight.Group
	locks   [64]sync.Mutex
}

type previewJob struct {
	id, channelID, guildID snowflake.ID
	content                string
	editedAt               *time.Time
	pending                []string
}

type previewSelection struct {
	embeds  []linkpreview.Embed
	pending []string
}

// decodeEmbeds accepts an unset row for the unpersisted message in Create.
func decodeEmbeds(data []byte) ([]linkpreview.Embed, error) {
	embeds := []linkpreview.Embed{}
	if len(data) != 0 {
		if err := json.Unmarshal(data, &embeds); err != nil {
			return nil, fmt.Errorf("messages: decode embeds: %w", err)
		}
	}
	return embeds, nil
}

// encodeEmbeds keeps the wire and database default as an empty array.
func encodeEmbeds(embeds []linkpreview.Embed) ([]byte, error) {
	if embeds == nil {
		embeds = []linkpreview.Embed{}
	}
	data, err := json.Marshal(embeds)
	if err != nil {
		return nil, fmt.Errorf("messages: encode embeds: %w", err)
	}
	return data, nil
}

// cachedPreviews returns fresh successes and failures for the selected URLs.
func (s *Service) cachedPreviews(ctx context.Context, q store.DBTX, urls []string) ([]store.CachedPreviewsRow, error) {
	now := s.now()
	rows, err := s.q.CachedPreviews(ctx, q, store.CachedPreviewsParams{
		Urls: urls, SuccessCutoff: now.Add(-previewSuccessTTL), FailureCutoff: now.Add(-previewFailureTTL),
	})
	if err != nil {
		return nil, fmt.Errorf("messages: read preview cache: %w", err)
	}
	return rows, nil
}

// selectPreviews retains existing embeds and fills gaps with fresh cache hits.
func (s *Service) selectPreviews(ctx context.Context, tx pgx.Tx, content string, existing []byte) (previewSelection, error) {
	var out previewSelection
	urls := previewURLs(content)
	if len(urls) == 0 {
		return out, nil
	}
	if err := s.q.LockPreviewReferences(ctx, tx); err != nil {
		return out, fmt.Errorf("messages: lock preview references: %w", err)
	}
	old, err := decodeEmbeds(existing)
	if err != nil {
		return out, err
	}
	cached, err := s.cachedPreviews(ctx, tx, urls)
	if err != nil {
		return out, err
	}
	for _, url := range urls {
		if i := slices.IndexFunc(old, func(e linkpreview.Embed) bool { return e.URL == url }); i >= 0 {
			out.embeds = append(out.embeds, old[i])
			continue
		}
		i := slices.IndexFunc(cached, func(c store.CachedPreviewsRow) bool { return c.Url == url })
		if i < 0 {
			out.pending = append(out.pending, url)
			continue
		}
		if cached[i].Status {
			var embed linkpreview.Embed
			if err := json.Unmarshal(cached[i].Embed, &embed); err != nil {
				return out, fmt.Errorf("messages: decode cached preview: %w", err)
			}
			out.embeds = append(out.embeds, embed)
		}
	}
	return out, nil
}

// lockPreviewUpdate orders asynchronous publication with edits and deletions.
func (s *Service) lockPreviewUpdate(id snowflake.ID) func() {
	if s.previews == nil {
		return func() {}
	}
	lock := &s.previews.locks[int64(id)&int64(len(s.previews.locks)-1)]
	lock.Lock()
	return lock.Unlock
}

// enqueuePreviews leaves the message usable if the bounded queue is full.
func (s *Service) enqueuePreviews(m Message, pending []string) {
	if s.previews == nil || len(pending) == 0 {
		return
	}
	select {
	case s.previews.jobs <- previewJob{id: m.ID, channelID: m.ChannelID, guildID: m.GuildID, content: m.Content, editedAt: m.EditedAt, pending: pending}:
	default:
	}
}

// RunPreviews starts fetch workers and sweeps expired cache rows until shutdown.
func (s *Service) RunPreviews(ctx context.Context) {
	if s.previews == nil {
		return
	}
	var workers sync.WaitGroup
	for range previewWorkers {
		workers.Go(func() { s.previewWorker(ctx) })
	}
	defer workers.Wait()
	tick := time.NewTicker(previewSweepInterval)
	defer tick.Stop()
	for ctx.Err() == nil {
		if err := s.sweepPreviews(ctx); err != nil && ctx.Err() == nil {
			s.logger.Warn("sweep link previews", "err", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
		}
	}
}

// previewWorker drains the queue independently of the sending request.
func (s *Service) previewWorker(ctx context.Context) {
	for ctx.Err() == nil {
		select {
		case <-ctx.Done():
			return
		case job := <-s.previews.jobs:
			if err := s.processPreview(ctx, job); err != nil && ctx.Err() == nil {
				s.logger.Warn("update message previews", "message_id", job.id, "err", err)
			}
		}
	}
}

// previewCurrent excludes deleted messages and jobs superseded by edits.
func (s *Service) previewCurrent(ctx context.Context, job previewJob) (store.Message, bool, error) {
	row, err := s.load(ctx, job.channelID, job.id)
	if errors.Is(err, ErrNotFound) {
		return row, false, nil
	}
	if err != nil {
		return row, false, fmt.Errorf("messages: check preview revision: %w", err)
	}
	sameEdit := row.EditedAt == nil && job.editedAt == nil ||
		row.EditedAt != nil && job.editedAt != nil && row.EditedAt.Equal(*job.editedAt)
	return row, sameEdit && row.Content == job.content, nil
}

// processPreview fetches outside message locks and commits only the same revision.
func (s *Service) processPreview(ctx context.Context, job previewJob) error {
	for _, url := range job.pending {
		if _, current, err := s.previewCurrent(ctx, job); err != nil || !current {
			return err
		}
		if err := s.fetchPreview(ctx, url); err != nil {
			return err
		}
	}
	defer s.lockPreviewUpdate(job.id)()
	row, current, err := s.previewCurrent(ctx, job)
	if err != nil || !current {
		return err
	}
	m, err := s.completeOne(ctx, job.guildID, row, 0)
	if err != nil {
		return fmt.Errorf("messages: complete preview update: %w", err)
	}
	var wrote int64
	err = db.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		picked, selectErr := s.selectPreviews(ctx, tx, row.Content, row.Embeds)
		if selectErr != nil {
			return selectErr
		}
		m.Embeds = picked.embeds
		data, encodeErr := encodeEmbeds(picked.embeds)
		if encodeErr != nil {
			return encodeErr
		}
		var updateErr error
		wrote, updateErr = s.q.SetMessageEmbeds(ctx, tx, store.SetMessageEmbedsParams{
			Embeds: data, ID: job.id, ChannelID: job.channelID, Content: job.content, EditedAt: job.editedAt,
		})
		if updateErr != nil {
			return fmt.Errorf("messages: save message embeds: %w", updateErr)
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("messages: commit preview update: %w", err)
	}
	if wrote != 0 {
		s.publishMessageUpdate(ctx, m)
	}
	return nil
}

// fetchPreview shares one in-flight request per URL and caches both outcomes.
func (s *Service) fetchPreview(ctx context.Context, url string) error {
	_, err, _ := s.previews.shared.Do(url, func() (any, error) {
		rows, err := s.cachedPreviews(ctx, s.pool, []string{url})
		if err != nil || len(rows) > 0 {
			return nil, err
		}
		embed, outcome := s.previews.fetcher.Fetch(ctx, url, s.saveEmbedImage)
		if ctxErr := ctx.Err(); ctxErr != nil {
			return nil, fmt.Errorf("messages: fetch preview cancelled: %w", ctxErr)
		}
		if outcome.Stage != "" {
			// A friend's bad link is not an incident: DEBUG, and only the host.
			s.logger.DebugContext(ctx, "link preview incomplete", "host", linkpreview.Host(url),
				"stage", outcome.Stage, "reason", outcome.Reason, "partial", outcome.Partial)
		}
		fetchedAt := s.now()
		if embed != nil && outcome.Partial {
			// A card that lost its picture or title expires with the failures,
			// so a later send tries again soon instead of in a day.
			fetchedAt = fetchedAt.Add(previewFailureTTL - previewSuccessTTL)
		}
		var data []byte
		if embed != nil {
			data, err = json.Marshal(embed)
			if err != nil {
				return nil, fmt.Errorf("messages: encode preview result: %w", err)
			}
		}
		err = s.q.SavePreview(ctx, s.pool, store.SavePreviewParams{Url: url, FetchedAt: fetchedAt, Status: embed != nil, Embed: data})
		if err != nil {
			return nil, fmt.Errorf("messages: save preview result: %w", err)
		}
		return nil, nil
	})
	return err
}

// sweepPreviews expires cache rows and queues unreferenced images for durable trash moves.
func (s *Service) sweepPreviews(ctx context.Context) error {
	now := s.now()
	for {
		var removed int
		err := db.InTx(ctx, s.pool, func(tx pgx.Tx) error {
			if err := s.q.LockPreviewCleanup(ctx, tx); err != nil {
				return fmt.Errorf("messages: lock preview cleanup: %w", err)
			}
			if err := s.q.ExpirePreviews(ctx, tx, store.ExpirePreviewsParams{
				SuccessCutoff: now.Add(-previewSuccessTTL), FailureCutoff: now.Add(-previewFailureTTL),
			}); err != nil {
				return fmt.Errorf("messages: expire link previews: %w", err)
			}
			ids, err := s.q.UnusedEmbedImages(ctx, tx, now.Add(-previewSuccessTTL))
			if err != nil {
				return fmt.Errorf("messages: find unused preview images: %w", err)
			}
			for _, id := range ids {
				if err := s.q.ForgetEmbedImage(ctx, tx, id); err != nil {
					return fmt.Errorf("messages: queue unused preview image: %w", err)
				}
			}
			removed = len(ids)
			return nil
		})
		if err != nil {
			return fmt.Errorf("messages: commit preview cleanup: %w", err)
		}
		if removed == 0 {
			return nil
		}
	}
}
