package server

import (
	"context"
	"errors"
	"fmt"
	"time"

	"connectrpc.com/connect"

	pagereportv1 "github.com/dusan/page-report/gen/pagereport/v1"
	"github.com/dusan/page-report/internal/id"
	"github.com/dusan/page-report/internal/store"
)

const defaultContentType = "text/html; charset=utf-8"

// rpcService implements pagereportv1connect.PageServiceHandler.
type rpcService struct {
	s *Server
}

func (r *rpcService) GetAuthConfig(
	ctx context.Context,
	_ *connect.Request[pagereportv1.GetAuthConfigRequest],
) (*connect.Response[pagereportv1.GetAuthConfigResponse], error) {
	ac := r.s.authCfg.AuthConfig()
	return connect.NewResponse(&pagereportv1.GetAuthConfigResponse{
		Provider:       ac.Provider,
		Issuer:         ac.Issuer,
		ClientId:       ac.ClientID,
		Scopes:         ac.Scopes,
		DeviceEndpoint: ac.DeviceEndpoint,
		TokenEndpoint:  ac.TokenEndpoint,
	}), nil
}

func (r *rpcService) UploadPage(
	ctx context.Context,
	req *connect.Request[pagereportv1.UploadPageRequest],
) (*connect.Response[pagereportv1.UploadPageResponse], error) {
	content := req.Msg.GetContent()
	if len(content) == 0 {
		return nil, connect.NewError(connect.CodeInvalidArgument,
			errors.New("content must not be empty"))
	}
	if int64(len(content)) > r.s.cfg.MaxUploadBytes {
		return nil, connect.NewError(connect.CodeInvalidArgument,
			fmt.Errorf("content exceeds max upload size of %d bytes", r.s.cfg.MaxUploadBytes))
	}
	contentType := req.Msg.GetContentType()
	if contentType == "" {
		contentType = defaultContentType
	}
	identity, _ := IdentityFrom(ctx)
	createdBy := identity.Email
	if createdBy == "" {
		createdBy = identity.Login
	}
	if createdBy == "" {
		createdBy = identity.Subject
	}

	var pageID string
	for attempt := 0; ; attempt++ {
		newID, err := id.New()
		if err != nil {
			return nil, connect.NewError(connect.CodeInternal, err)
		}
		err = r.s.store.CreatePage(ctx, store.Page{
			ID:          newID,
			Title:       req.Msg.GetTitle(),
			Content:     content,
			ContentType: contentType,
			SizeBytes:   int64(len(content)),
			CreatedAt:   time.Now().UTC(),
			CreatedBy:   createdBy,
		})
		if err == nil {
			pageID = newID
			break
		}
		if store.IsDuplicateID(err) && attempt < 2 {
			continue
		}
		return nil, connect.NewError(connect.CodeInternal, err)
	}

	return connect.NewResponse(&pagereportv1.UploadPageResponse{
		Id:  pageID,
		Url: r.s.cfg.PageURL(pageID),
	}), nil
}

func (r *rpcService) ListPages(
	ctx context.Context,
	_ *connect.Request[pagereportv1.ListPagesRequest],
) (*connect.Response[pagereportv1.ListPagesResponse], error) {
	pages, err := r.s.store.ListPages(ctx)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	resp := &pagereportv1.ListPagesResponse{}
	for _, p := range pages {
		resp.Pages = append(resp.Pages, r.meta(p))
	}
	return connect.NewResponse(resp), nil
}

func (r *rpcService) GetPage(
	ctx context.Context,
	req *connect.Request[pagereportv1.GetPageRequest],
) (*connect.Response[pagereportv1.GetPageResponse], error) {
	p, err := r.s.store.GetPage(ctx, req.Msg.GetId())
	if errors.Is(err, store.ErrNotFound) {
		return nil, connect.NewError(connect.CodeNotFound, err)
	}
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	resp := &pagereportv1.GetPageResponse{Meta: r.meta(p)}
	if req.Msg.GetIncludeContent() {
		resp.Content = p.Content
	}
	return connect.NewResponse(resp), nil
}

func (r *rpcService) DeletePage(
	ctx context.Context,
	req *connect.Request[pagereportv1.DeletePageRequest],
) (*connect.Response[pagereportv1.DeletePageResponse], error) {
	err := r.s.store.DeletePage(ctx, req.Msg.GetId())
	if errors.Is(err, store.ErrNotFound) {
		return nil, connect.NewError(connect.CodeNotFound, err)
	}
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	return connect.NewResponse(&pagereportv1.DeletePageResponse{}), nil
}

func (r *rpcService) PrunePages(
	ctx context.Context,
	req *connect.Request[pagereportv1.PrunePagesRequest],
) (*connect.Response[pagereportv1.PrunePagesResponse], error) {
	olderThan := req.Msg.GetOlderThanSeconds()
	if olderThan <= 0 {
		return nil, connect.NewError(connect.CodeInvalidArgument,
			errors.New("older_than_seconds must be positive"))
	}
	cutoff := time.Now().Add(-time.Duration(olderThan) * time.Second)
	n, err := r.s.store.PrunePages(ctx, cutoff)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	return connect.NewResponse(&pagereportv1.PrunePagesResponse{DeletedCount: n}), nil
}

func (r *rpcService) meta(p store.Page) *pagereportv1.PageMeta {
	return &pagereportv1.PageMeta{
		Id:          p.ID,
		Title:       p.Title,
		ContentType: p.ContentType,
		SizeBytes:   p.SizeBytes,
		CreatedAt:   p.CreatedAt.Unix(),
		CreatedBy:   p.CreatedBy,
		Url:         r.s.cfg.PageURL(p.ID),
	}
}
