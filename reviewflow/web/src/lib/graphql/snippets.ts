import { gql } from '@urql/svelte';

/** Shared fragments keep the field sets consistent across documents. */
const USER_FIELDS = gql`
	fragment UserFields on User {
		id
		githubUsername
		avatarUrl
		createdAt
	}
`;

const REVIEW_FIELDS = gql`
	fragment ReviewFields on Review {
		id
		snippetId
		body
		lineNumber
		parentReviewId
		createdAt
		author {
			...UserFields
		}
	}
	${USER_FIELDS}
`;

const AI_REVIEW_FIELDS = gql`
	fragment AIReviewFields on AIReview {
		id
		snippetId
		suggestions
		complexity
		refactorHints
		securityFlags
		qualityScore
		language
		generatedAt
	}
`;

/** Full snippet for the detail page (code + reviews + AI review). */
export const GET_SNIPPET = gql`
	query GetSnippet($id: ID!) {
		snippet(id: $id) {
			id
			title
			language
			code
			previousVersion
			createdAt
			updatedAt
			author {
				...UserFields
			}
			reviews {
				...ReviewFields
			}
			aiReview {
				...AIReviewFields
			}
		}
	}
	${USER_FIELDS}
	${REVIEW_FIELDS}
	${AI_REVIEW_FIELDS}
`;

/** Lightweight list for the dashboard grid. */
export const GET_SNIPPETS = gql`
	query GetSnippets($authorId: ID, $language: String, $limit: Int) {
		snippets(authorId: $authorId, language: $language, limit: $limit) {
			id
			title
			language
			createdAt
			author {
				githubUsername
				avatarUrl
			}
			reviews {
				id
			}
			aiReview {
				id
			}
		}
	}
`;

export const CREATE_SNIPPET = gql`
	mutation CreateSnippet($input: CreateSnippetInput!) {
		createSnippet(input: $input) {
			id
			title
			language
		}
	}
`;

export const ADD_REVIEW = gql`
	mutation AddReview($snippetId: ID!, $input: AddReviewInput!) {
		addReview(snippetId: $snippetId, input: $input) {
			...ReviewFields
		}
	}
	${REVIEW_FIELDS}
`;
