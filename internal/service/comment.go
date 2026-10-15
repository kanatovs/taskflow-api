package service

import (
	"context"
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/kanatovs/taskflow-api/internal/domain"
)

type CommentRepository interface {
	Create(context.Context, int64, domain.Comment) (domain.Comment, error)
	ByID(context.Context, int64, int64, int64) (domain.Comment, error)
	List(context.Context, int64, int64, int, int) ([]domain.Comment, int, error)
	Update(context.Context, int64, int64, int64, int64, string) (domain.Comment, error)
	Delete(context.Context, int64, int64, int64, int64) error
}

type CommentService struct {
	projects *ProjectService
	tasks    TaskRepository
	comments CommentRepository
}

func NewCommentService(projects *ProjectService, tasks TaskRepository, comments CommentRepository) *CommentService {
	return &CommentService{projects: projects, tasks: tasks, comments: comments}
}

type CommentInput struct {
	Body string `json:"body"`
}

func (s *CommentService) Create(ctx context.Context, actorID, projectID, taskID int64, input CommentInput) (domain.Comment, error) {
	if _, err := s.access(ctx, actorID, projectID, taskID); err != nil {
		return domain.Comment{}, err
	}
	body, err := validateCommentBody(input.Body)
	if err != nil {
		return domain.Comment{}, err
	}
	comment, err := s.comments.Create(ctx, projectID, domain.Comment{TaskID: taskID, AuthorID: actorID, Body: body})
	if err != nil {
		return domain.Comment{}, fmt.Errorf("create comment: %w", err)
	}
	return comment, nil
}

func (s *CommentService) List(ctx context.Context, actorID, projectID, taskID int64, page, size int) ([]domain.Comment, int, error) {
	if _, err := s.access(ctx, actorID, projectID, taskID); err != nil {
		return nil, 0, err
	}
	if err := validatePage(page, size); err != nil {
		return nil, 0, err
	}
	comments, total, err := s.comments.List(ctx, projectID, taskID, page, size)
	if err != nil {
		return nil, 0, fmt.Errorf("list comments: %w", err)
	}
	return comments, total, nil
}

func (s *CommentService) Update(ctx context.Context, actorID, projectID, taskID, commentID int64, input CommentInput) (domain.Comment, error) {
	if err := s.manage(ctx, actorID, projectID, taskID, commentID); err != nil {
		return domain.Comment{}, err
	}
	body, err := validateCommentBody(input.Body)
	if err != nil {
		return domain.Comment{}, err
	}
	comment, err := s.comments.Update(ctx, projectID, taskID, commentID, actorID, body)
	if err != nil {
		return domain.Comment{}, fmt.Errorf("update comment: %w", err)
	}
	return comment, nil
}

func (s *CommentService) Delete(ctx context.Context, actorID, projectID, taskID, commentID int64) error {
	if err := s.manage(ctx, actorID, projectID, taskID, commentID); err != nil {
		return err
	}
	if err := s.comments.Delete(ctx, projectID, taskID, commentID, actorID); err != nil {
		return fmt.Errorf("delete comment: %w", err)
	}
	return nil
}

func (s *CommentService) access(ctx context.Context, actorID, projectID, taskID int64) (domain.ProjectRole, error) {
	_, role, err := s.projects.access(ctx, actorID, projectID)
	if err != nil {
		return "", err
	}
	if err := validateTaskID(taskID); err != nil {
		return "", err
	}
	if _, err := s.tasks.ByID(ctx, projectID, taskID); err != nil {
		return "", fmt.Errorf("find comment task: %w", err)
	}
	return role, nil
}

func (s *CommentService) manage(ctx context.Context, actorID, projectID, taskID, commentID int64) error {
	role, err := s.access(ctx, actorID, projectID, taskID)
	if err != nil {
		return err
	}
	if commentID <= 0 {
		return &domain.ValidationError{Fields: map[string]string{"comment_id": "must be a positive integer"}}
	}
	comment, err := s.comments.ByID(ctx, projectID, taskID, commentID)
	if err != nil {
		return fmt.Errorf("find managed comment: %w", err)
	}
	if !domain.CanManageComment(role, actorID, comment) {
		return domain.ErrForbidden
	}
	return nil
}

func validateCommentBody(body string) (string, error) {
	body = strings.TrimSpace(body)
	if !utf8.ValidString(body) || utf8.RuneCountInString(body) < 1 || utf8.RuneCountInString(body) > 2000 || strings.IndexFunc(body, func(r rune) bool { return unicode.IsControl(r) && r != '\n' && r != '\r' && r != '\t' }) >= 0 {
		return "", &domain.ValidationError{Fields: map[string]string{"body": "must contain 1 to 2000 characters without unsupported control characters"}}
	}
	return body, nil
}
