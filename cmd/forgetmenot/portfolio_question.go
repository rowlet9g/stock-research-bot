package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const (
	defaultPortfolioQuestionFile = "prompts/portfolio_review.md"
	maxPortfolioQuestionBytes    = 64 * 1024
)

func resolvePortfolioQuestion(
	inlineQuestion string,
	questionFile string,
) (string, error) {
	inlineQuestion = strings.TrimSpace(inlineQuestion)
	if inlineQuestion != "" {
		return inlineQuestion, nil
	}

	questionFile = strings.TrimSpace(questionFile)
	if questionFile == "" {
		return "", nil
	}
	resolvedPath, err := resolvePortfolioQuestionPath(questionFile)
	if err != nil {
		return "", err
	}
	content, err := os.ReadFile(resolvedPath)
	if err != nil {
		return "", fmt.Errorf(
			"read portfolio question file %q: %w",
			resolvedPath,
			err,
		)
	}
	if len(content) > maxPortfolioQuestionBytes {
		return "", fmt.Errorf(
			"portfolio question file %q exceeds %d bytes",
			resolvedPath,
			maxPortfolioQuestionBytes,
		)
	}
	question := strings.TrimSpace(
		strings.TrimPrefix(string(content), "\uFEFF"),
	)
	if question == "" {
		return "", fmt.Errorf(
			"portfolio question file %q is empty",
			resolvedPath,
		)
	}
	return question, nil
}

func resolvePortfolioQuestionPath(questionFile string) (string, error) {
	if filepath.IsAbs(questionFile) ||
		filepath.Clean(questionFile) !=
			filepath.Clean(defaultPortfolioQuestionFile) {
		return questionFile, nil
	}

	workingDirectory, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf(
			"resolve portfolio question working directory: %w",
			err,
		)
	}
	for directory := workingDirectory; ; directory = filepath.Dir(directory) {
		candidate := filepath.Join(directory, questionFile)
		info, statErr := os.Stat(candidate)
		switch {
		case statErr == nil && !info.IsDir():
			return candidate, nil
		case statErr == nil:
			return "", fmt.Errorf(
				"portfolio question path %q is a directory",
				candidate,
			)
		case !os.IsNotExist(statErr):
			return "", fmt.Errorf(
				"inspect portfolio question file %q: %w",
				candidate,
				statErr,
			)
		}

		parent := filepath.Dir(directory)
		if parent == directory {
			break
		}
	}
	return "", fmt.Errorf(
		"default portfolio question file %q was not found; "+
			"use -question-file with an explicit path or "+
			"-question-file \"\" to use the built-in request",
		questionFile,
	)
}
