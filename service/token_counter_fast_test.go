package service

import (
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/tokenkit"
	"github.com/stretchr/testify/require"
)

func TestFastResponsesTokenEstimateTracksLargeText(t *testing.T) {
	text := strings.Repeat("This is a long Responses prompt with cached context and tool metadata. ", 16000)
	info := &common.RelayInfo{
		RelayFormat:     types.RelayFormatOpenAIResponses,
		OriginModelName: "gpt-4o",
	}
	meta := &types.TokenCountMeta{CombineText: text, ToolsCount: 2, MessagesCount: 3}

	estimate, ok := fastResponsesTokenEstimate(meta, info)
	require.True(t, ok)
	exact := tokenkit.Count(info.OriginModelName, text) + meta.ToolsCount*8 + meta.MessagesCount*3 + 3
	require.InDelta(t, exact, estimate, float64(exact)*0.15)
}

func TestFastResponsesTokenEstimateSkipsUnsafeInputs(t *testing.T) {
	info := &common.RelayInfo{
		RelayFormat:     types.RelayFormatOpenAIResponses,
		OriginModelName: "gpt-4o",
	}
	short := &types.TokenCountMeta{CombineText: strings.Repeat("x", fastResponsesTokenEstimateMinBytes-1)}
	_, ok := fastResponsesTokenEstimate(short, info)
	require.False(t, ok)

	withMedia := &types.TokenCountMeta{
		CombineText: strings.Repeat("x", fastResponsesTokenEstimateMinBytes),
		Files:       []*types.FileMeta{{FileType: types.FileTypeImage}},
	}
	_, ok = fastResponsesTokenEstimate(withMedia, info)
	require.False(t, ok)

	otherFormat := &common.RelayInfo{RelayFormat: types.RelayFormatOpenAI, OriginModelName: "gpt-4o"}
	_, ok = fastResponsesTokenEstimate(withMedia, otherFormat)
	require.False(t, ok)
}
