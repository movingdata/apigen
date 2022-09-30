package main

import (
	"go/types"
	"io"
	"text/template"
)

type JSWriter struct{ Dir string }

func NewJSWriter(dir string) *JSWriter { return &JSWriter{Dir: dir} }

func (JSWriter) Name() string     { return "js" }
func (JSWriter) Language() string { return "js" }
func (w JSWriter) File(typeName string, namedType *types.Named, structType *types.Struct) string {
	model, err := makeModel(typeName, namedType, structType)
	if err != nil {
		panic(err)
	}
	return w.Dir + "/" + model.LowerPlural + ".js"
}

func (JSWriter) Imports(typeName string, namedType *types.Named, structType *types.Struct) []string {
	return []string{}
}

func (w *JSWriter) Write(wr io.Writer, typeName string, namedType *types.Named, structType *types.Struct) error {
	model, err := makeModel(typeName, namedType, structType)
	if err != nil {
		return err
	}
	return jsTemplate.Execute(wr, *model)
}

var jsTemplate = template.Must(template.New("jsTemplate").Funcs(tplFunc).Parse(`
// @flow

{{$Type := .}}

import axios from 'axios';
import { useContext, useEffect } from 'react';
import { useDispatch, useSelector } from 'react-redux';
import URLSearchParams from 'url-search-params';

import {
  invalidateFetchCacheWithIDs,
  invalidateSearchCacheWithIDs,
  makeSearchKey,
  updateFetchCacheCompleteMulti,
  updateFetchCacheErrorMulti,
  updateFetchCacheLoading,
  updateFetchCachePushMulti,
  updateSearchCacheComplete,
  updateSearchCacheError,
  updateSearchCacheLoading,
} from 'lib/duckHelpers';
import type { FetchCache, SearchCache, SearchPageKey } from 'lib/duckHelpers';
import mergeArrays from 'lib/mergeArrays';
import { Context as SubscriptionsContext } from 'lib/subscriptions';

import { errorsEnsureError } from './errors';
import type { ErrorResponse } from './errors';

{{range $Field := $Type.Fields}}
{{if $Field.Enum}}
export type {{$Type.Singular}}{{$Field.GoName}} =
{{- range $Enum := $Field.Enum}}
  | "{{$Enum.Value}}"
{{- end}}

{{range $Enum := $Field.Enum}}
export const {{$Type.LowerPlural}}Enum{{$Field.GoName}}{{$Enum.GoName}} = '{{$Enum.Value}}';
{{- end}}

export const {{$Type.LowerPlural}}Values{{$Field.GoName}}: $ReadOnlyArray<{{$Type.Singular}}{{$Field.GoName}}> = [
{{- range $Enum := $Field.Enum}}
  {{$Type.LowerPlural}}Enum{{$Field.GoName}}{{$Enum.GoName}},
{{- end}}
];

export const {{$Type.LowerPlural}}Labels{{$Field.GoName}}: { [key: {{$Type.Singular}}{{$Field.GoName}}]: string } = {
{{- range $Enum := $Field.Enum}}
  [{{$Type.LowerPlural}}Enum{{$Field.GoName}}{{$Enum.GoName}}]: '{{$Enum.Label}}',
{{- end}}
}
{{- end}}
{{- end}}

const defaultPageSize = 10;

/** {{$Type.Singular}} is a complete {{$Type.Singular}} object */
export type {{$Type.Singular}} = {|
{{- range $Field := $Type.Fields}}
  {{$Field.APIName}}: {{$Field.JSType}},
{{- end}}
|};

{{if $Type.CanCreate}}
/** {{$Type.Singular}}CreateInput is the data needed to call {{$Type.LowerPlural}}Create */
export type {{$Type.Singular}}CreateInput = {|
{{- range $Field := $Type.Fields}}
{{- if not (or $Field.IgnoreInput) }}
  {{$Field.APIName}}{{if $Field.OmitEmpty}}?{{end}}: {{$Field.JSType}},
{{- end}}
{{- end}}
|};
{{end}}

/** {{$Type.Singular}}SearchParams is used to call {{$Type.LowerPlural}}Search */
export type {{$Type.Singular}}SearchParams = {|
{{- range $Field := $Type.Fields}}
{{- range $Filter := $Field.Filters}}
  {{$Filter.Name}}?: {{$Filter.JSType}},
{{- end}}
{{- end}}
{{- range $Filter := $Type.SpecialFilters}}
  {{$Filter.Name}}?: {{$Filter.JSType}},
{{- end}}
  order?: string,
  pageSize?: number,
  page: SearchPageKey,
|};

export type State = {
  loading: number,
  {{$Type.LowerPlural}}: $ReadOnlyArray<{{$Type.Singular}}>,
  error: ?ErrorResponse,
  searchCache: SearchCache<{{$Type.Singular}}SearchParams>,
  fetchCache: FetchCache,
  timeouts: { [key: string]: ?TimeoutID },
};

{{if (or $Type.CanCreate $Type.CanUpdate)}}
type Invalidator = (c: SearchCache<{{$Type.Singular}}SearchParams>) => SearchCache<{{$Type.Singular}}SearchParams>;
{{end}}

{{if $Type.CanCreate}}
type {{$Type.Singular}}CreateOptions = {
  invalidate?: boolean | Invalidator,
  after?: (err: ?Error, record?: {{$Type.Singular}}) => void,
  push?: boolean,
};

type {{$Type.Singular}}CreateMultipleOptions = {
  invalidate?: boolean | Invalidator,
  after?: (err: ?Error, records?: $ReadOnlyArray<{{$Type.Singular}}>) => void,
  push?: boolean,
  timeout?: number,
};
{{end}}

{{if $Type.CanUpdate}}
type {{$Type.Singular}}UpdateOptions = {
  invalidate?: boolean | Invalidator,
  after?: (err: ?Error, record?: {{$Type.Singular}}) => void,
  push?: boolean,
  timeout?: number,
};

type {{$Type.Singular}}UpdateMultipleOptions = {
  invalidate?: boolean | Invalidator,
  after?: (err: ?Error, records?: $ReadOnlyArray<{{$Type.Singular}}>) => void,
  push?: boolean,
  timeout?: number,
};
{{end}}

export const actionCreateBegin = 'X/{{Hash $Type.LowerPlural "/CREATE_BEGIN"}}';
export const actionCreateComplete = 'X/{{Hash $Type.LowerPlural "/CREATE_COMPLETE"}}';
export const actionCreateFailed = 'X/{{Hash $Type.LowerPlural "/CREATE_FAILED"}}';
export const actionCreateMultipleBegin = 'X/{{Hash $Type.LowerPlural "/CREATE_MULTIPLE_BEGIN"}}';
export const actionCreateMultipleComplete = 'X/{{Hash $Type.LowerPlural "/CREATE_MULTIPLE_COMPLETE"}}';
export const actionCreateMultipleFailed = 'X/{{Hash $Type.LowerPlural "/CREATE_MULTIPLE_FAILED"}}';
export const actionFetchBegin = 'X/{{Hash $Type.LowerPlural "/FETCH_BEGIN"}}';
export const actionFetchCompleteMulti = 'X/{{Hash $Type.LowerPlural "/FETCH_COMPLETE_MULTI"}}';
export const actionFetchFailedMulti = 'X/{{Hash $Type.LowerPlural "/FETCH_FAILED_MULTI"}}';
export const actionReset = 'X/{{Hash $Type.LowerPlural "/RESET"}}';
export const actionSearchBegin = 'X/{{Hash $Type.LowerPlural "/SEARCH_BEGIN"}}';
export const actionSearchComplete = 'X/{{Hash $Type.LowerPlural "/SEARCH_COMPLETE"}}';
export const actionSearchFailed = 'X/{{Hash $Type.LowerPlural "/SEARCH_FAILED"}}';
export const actionUpdateBegin = 'X/{{Hash $Type.LowerPlural "/UPDATE_BEGIN"}}';
export const actionUpdateCancel = 'X/{{Hash $Type.LowerPlural "/UPDATE_CANCEL"}}';
export const actionUpdateComplete = 'X/{{Hash $Type.LowerPlural "/UPDATE_COMPLETE"}}';
export const actionUpdateFailed = 'X/{{Hash $Type.LowerPlural "/UPDATE_FAILED"}}';
export const actionUpdateMultipleBegin = 'X/{{Hash $Type.LowerPlural "/UPDATE_MULTIPLE_BEGIN"}}';
export const actionUpdateMultipleCancel = 'X/{{Hash $Type.LowerPlural "/UPDATE_MULTIPLE_CANCEL"}}';
export const actionUpdateMultipleComplete = 'X/{{Hash $Type.LowerPlural "/UPDATE_MULTIPLE_COMPLETE"}}';
export const actionUpdateMultipleFailed = 'X/{{Hash $Type.LowerPlural "/UPDATE_MULTIPLE_FAILED"}}';
export const actionInvalidateCache = 'X/{{Hash $Type.LowerPlural "/INVALIDATE_CACHE"}}';
export const actionRecordPush = 'X/{{Hash $Type.LowerPlural "/RECORD_PUSH"}}';
export const actionRecordPushMulti = 'X/{{Hash $Type.LowerPlural "/RECORD_PUSH_MULTI"}}';

export type Action =
  | {
      type: 'X/{{Hash $Type.LowerPlural "/SEARCH_BEGIN"}}',
      payload: { params: {{$Type.Singular}}SearchParams, key: string, page: SearchPageKey },
    }
  | {
      type: 'X/{{Hash $Type.LowerPlural "/SEARCH_COMPLETE"}}',
      payload: {
        records: $ReadOnlyArray<{{$Type.Singular}}>,
        total: number,
        time: number,
        params: {{$Type.Singular}}SearchParams,
        key: string,
        page: SearchPageKey,
      },
    }
  | {
      type: 'X/{{Hash $Type.LowerPlural "/SEARCH_FAILED"}}',
      payload: {
        time: number,
        params: {{$Type.Singular}}SearchParams,
        key: string,
        page: SearchPageKey,
        error: ErrorResponse,
      },
    }
  | { type: 'X/{{Hash $Type.LowerPlural "/FETCH_BEGIN"}}', payload: { id: string } }
  | {
      type: 'X/{{Hash $Type.LowerPlural "/FETCH_COMPLETE_MULTI"}}',
      payload: { ids: $ReadOnlyArray<string>, time: number, records: $ReadOnlyArray<{{$Type.Singular}}> },
    }
  | {
      type: 'X/{{Hash $Type.LowerPlural "/FETCH_FAILED_MULTI"}}',
      payload: { ids: $ReadOnlyArray<string>, time: number, error: ErrorResponse },
    }
{{if $Type.CanCreate}}
  | {
      type: 'X/{{Hash $Type.LowerPlural "/CREATE_BEGIN"}}',
      payload: { record: {{$Type.Singular}} },
    }
  | {
      type: 'X/{{Hash $Type.LowerPlural "/CREATE_COMPLETE"}}',
      payload: { record: {{$Type.Singular}}, options: {{$Type.Singular}}CreateOptions },
    }
  | {
      type: 'X/{{Hash $Type.LowerPlural "/CREATE_FAILED"}}',
      payload: { error: ErrorResponse },
    }
  | {
      type: 'X/{{Hash $Type.LowerPlural "/CREATE_MULTIPLE_BEGIN"}}',
      payload: { records: $ReadOnlyArray<{{$Type.Singular}}>, options: {{$Type.Singular}}CreateMultipleOptions },
    }
  | {
      type: 'X/{{Hash $Type.LowerPlural "/CREATE_MULTIPLE_COMPLETE"}}',
      payload: { records: $ReadOnlyArray<{{$Type.Singular}}>, options: {{$Type.Singular}}CreateMultipleOptions },
    }
  | {
      type: 'X/{{Hash $Type.LowerPlural "/CREATE_MULTIPLE_FAILED"}}',
      payload: { records: $ReadOnlyArray<{{$Type.Singular}}>, options: {{$Type.Singular}}CreateMultipleOptions, error: ErrorResponse },
    }
{{end}}
{{if $Type.CanUpdate}}
  | {
      type: 'X/{{Hash $Type.LowerPlural "/UPDATE_BEGIN"}}',
      payload: { record: {{$Type.Singular}}, timeout: number },
    }
  | { type: 'X/{{Hash $Type.LowerPlural "/UPDATE_CANCEL"}}', payload: { id: string } }
  | {
      type: 'X/{{Hash $Type.LowerPlural "/UPDATE_COMPLETE"}}',
      payload: { record: {{$Type.Singular}}, options: {{$Type.Singular}}UpdateOptions },
    }
  | {
      type: 'X/{{Hash $Type.LowerPlural "/UPDATE_FAILED"}}',
      payload: { record: {{$Type.Singular}}, error: ErrorResponse },
    }
  | {
      type: 'X/{{Hash $Type.LowerPlural "/UPDATE_MULTIPLE_BEGIN"}}',
      payload: { records: $ReadOnlyArray<{{$Type.Singular}}>, timeout: number },
    }
  | { type: 'X/{{Hash $Type.LowerPlural "/UPDATE_MULTIPLE_CANCEL"}}', payload: { ids: string } }
  | {
      type: 'X/{{Hash $Type.LowerPlural "/UPDATE_MULTIPLE_COMPLETE"}}',
      payload: { records: $ReadOnlyArray<{{$Type.Singular}}>, options: {{$Type.Singular}}UpdateMultipleOptions },
    }
  | {
      type: 'X/{{Hash $Type.LowerPlural "/UPDATE_MULTIPLE_FAILED"}}',
      payload: { records: $ReadOnlyArray<{{$Type.Singular}}>, error: ErrorResponse },
    }
{{end}}
  | { type: 'X/{{Hash $Type.LowerPlural "/RESET"}}', payload: {} }
  | { type: 'X/{{Hash $Type.LowerPlural "/INVALIDATE_CACHE"}}', payload: {} }
  | { type: 'X/{{Hash $Type.LowerPlural "/RECORD_PUSH"}}', payload: { time: number, record: {{$Type.Singular}} } }
  | { type: 'X/{{Hash $Type.LowerPlural "/RECORD_PUSH_MULTI"}}', payload: { time: number, records: $ReadOnlyArray<{{$Type.Singular}}> } }
  | { type: 'X/INVALIDATE', payload: { {{$Type.Singular}}?: $ReadOnlyArray<string> } }
{{if $Type.HasVersion}}
  | { type: 'X/INVALIDATE_OUTDATED', payload: { {{$Type.Singular}}?: $ReadOnlyArray<[string, number]> } }
{{end}}
  | { type: 'X/RECORD_PUSH_MULTI', payload: { time: number, changed: { {{$Type.Singular}}?: $ReadOnlyArray<{{$Type.Singular}}> } } };

/** {{$Type.LowerPlural}}Search */
export function {{$Type.LowerPlural}}Search(params: {{$Type.Singular}}SearchParams): (dispatch: (ev: any) => void) => void {
  return function(dispatch: (ev: any) => void): void {
    const p = new URLSearchParams();

    for (const k of Object.keys(params).sort()) {
      if (k === 'page' || k === 'pageSize') { continue; }

      const v: any = params[k];

      if (Array.isArray(v)) {
        p.set(k, v.slice().sort().join(','));
      } else if (typeof v === 'string' || typeof v === 'number' || typeof v === 'boolean') {
        p.set(k, v);
      }
    }

    let pageSize: number = defaultPageSize;
    const inputPageSize = params.pageSize;
    if (typeof inputPageSize === 'number' && !Number.isNaN(inputPageSize)) {
      pageSize = inputPageSize;
    }

    const inputPage = params.page;
    if (typeof inputPage === 'number' && !Number.isNaN(inputPage)) {
      p.set('offset', (inputPage - 1) * pageSize);
      p.set('limit', pageSize);
    }

    const key = makeSearchKey(params);

    dispatch({
      type: 'X/{{Hash $Type.LowerPlural "/SEARCH_BEGIN"}}',
      payload: { params, key, page: params.page },
    });

    axios.get('/api/{{$Type.LowerPlural}}?' + p.toString()).then(
      ({ data: { records, total, time } }: {
        data: { records: $ReadOnlyArray<{{$Type.Singular}}>, total: number, time: string },
      }) => void dispatch({
        type: 'X/{{Hash $Type.LowerPlural "/SEARCH_COMPLETE"}}',
        payload: { records, total, time: new Date(time).valueOf(), params, key, page: params.page },
      }),
      (err: Error) => {
        dispatch({
          type: 'X/{{Hash $Type.LowerPlural "/SEARCH_FAILED"}}',
          payload: {
            params,
            key,
            page: params.page,
            time: Date.now(),
            error: errorsEnsureError(err),
          },
        });
      }
    );
  };
}

/** {{$Type.LowerPlural}}SearchIfRequired will only perform a search if the current results are older than the specified ttl, which is one minute by default */
export function {{$Type.LowerPlural}}SearchIfRequired(
  params: {{$Type.Singular}}SearchParams,
  ttl: number = 1000 * 60,
  now: Date = new Date()
): (dispatch: (ev: any) => void, getState: () => { {{$Type.LowerPlural}}: State }) => void {
  return function(dispatch: (ev: any) => void, getState: () => { {{$Type.LowerPlural}}: State }): void {
    const { {{$Type.LowerPlural}}: { searchCache } } = getState();

    const k = makeSearchKey(params);

    let refresh = false;

    const c = searchCache[k];

    if (c) {
      const { pages } = c;

      const page = pages[String(params.page)];

      if (!page) {
        refresh = true;
      } else if (page.time) {
        if (!page.loading && now.valueOf() - page.time > ttl) {
          refresh = true;
        }
      } else {
        if (!page.loading) {
          refresh = true;
        }
      }
    } else {
      refresh = true;
    }

    if (refresh) {
      dispatch({{$Type.LowerPlural}}Search(params));
    }
  };
}

/** {{$Type.LowerPlural}}GetSearchRecords fetches the {{$Type.Singular}} objects related to a specific search query, if available */
export function {{$Type.LowerPlural}}GetSearchRecords(
  state: State,
  params: {{$Type.Singular}}SearchParams
): ?$ReadOnlyArray<{{$Type.Singular}}> {
  const k = makeSearchKey(params);

  const c = state.searchCache[k];
  if (!c || !c.pages) {
    return null;
  }

  const p = c.pages[String(params.page)];
  if (!p || !p.items) {
    return null;
  }

  return p.items.map(id =>
    state.{{$Type.LowerPlural}}.find(e => e.id === id)
  ).reduce((arr, e) => e ? [ ...arr, e ] : arr, ([]: $ReadOnlyArray<{{$Type.Singular}}>));
}

/** {{$Type.LowerPlural}}GetSearchMeta fetches the metadata related to a specific search query, if available */
export function {{$Type.LowerPlural}}GetSearchMeta(
  state: State,
  params: {{$Type.Singular}}SearchParams
): ?{ time: number, total: number, loading: number } {
  const k = makeSearchKey(params);

  const c = state.searchCache[k];
  if (!c || !c.pages) {
    return null;
  }

  const p = c.pages[String(params.page)];

  return { time: c.time, total: c.total, loading: p ? p.loading : 0 };
}

/** {{$Type.LowerPlural}}GetSearchLoading returns the loading status for a specific search query */
export function {{$Type.LowerPlural}}GetSearchLoading(
  state: State,
  params: {{$Type.Singular}}SearchParams
): boolean {
  const k = makeSearchKey(params);

  const c = state.searchCache[k];
  if (!c || !c.pages) {
    return false;
  }

  const p = c.pages[String(params.page)];
  if (!p) {
    return false;
  }

  return p.loading > 0;
}

export type {{$Type.Singular}}SearchModifier = (params: {{$Type.Singular}}SearchParams) => {{$Type.Singular}}SearchParams;

/** use{{$Type.Singular}}Search forms a react hook for a specific search query */
export function use{{$Type.Singular}}Search(params: {{$Type.Singular}}SearchParams, ...modifiers: Array<{{$Type.Singular}}SearchModifier>): {
  meta: ?{ time: number, total: number, loading: number },
  loading: boolean,
  records: $ReadOnlyArray<{{$Type.Singular}}>,
} {
  const modified = modifiers.reduce((p, fn) => fn(p), params);

  const dispatch = useDispatch();
  useEffect(() => void dispatch({{$Type.LowerPlural}}SearchIfRequired(modified)));
  const { meta, loading, records } = useSelector(({ {{$Type.LowerPlural}} }: { {{$Type.LowerPlural}}: State }) => ({
    meta: {{$Type.LowerPlural}}GetSearchMeta({{$Type.LowerPlural}}, modified),
    loading: {{$Type.LowerPlural}}GetSearchLoading({{$Type.LowerPlural}}, modified) || !{{$Type.LowerPlural}}GetSearchMeta({{$Type.LowerPlural}}, modified),
    records: {{$Type.LowerPlural}}GetSearchRecords({{$Type.LowerPlural}}, modified) || [],
  }));

  const manager = useContext(SubscriptionsContext);
  const ids = records.map(e => e.id).sort().join(',');
  useEffect(() => {
    if (!manager || !ids) { return }
    ids.split(',').forEach(id => manager.inc('{{$Type.Singular}}', id));

    return () => {
      if (!manager || !ids) { return }
      ids.split(',').forEach(id => manager.dec('{{$Type.Singular}}', id));
    }
  }, [manager, ids]);

  return { meta, loading, records };
}

/** pendingFetch is a module-level metadata cache for ongoing fetch operations */ 
const pendingFetch: {
  timeout: ?TimeoutID,
  ids: $ReadOnlyArray<string>,
} = {
  timeout: null,
  ids: [],
};

function batchFetch(id: string, dispatch: (ev: any) => void) {
  if (pendingFetch.timeout === null) {
    pendingFetch.timeout = setTimeout(() => {
      const { ids } = pendingFetch;

      pendingFetch.timeout = null;
      pendingFetch.ids = [];

      axios.get('/api/{{$Type.LowerPlural}}?idIn=' + ids.join(',')).then(
        ({ data: { records }, }: { data: { records: $ReadOnlyArray<{{$Type.Singular}}> } }) => {
          dispatch({
            type: 'X/{{Hash $Type.LowerPlural "/FETCH_COMPLETE_MULTI"}}',
            payload: { ids, time: Date.now(), records },
          });
        },
        (err) => {
          dispatch({
            type: 'X/{{Hash $Type.LowerPlural "/FETCH_FAILED_MULTI"}}',
            payload: { ids, time: Date.now(), error: errorsEnsureError(err) },
          });
        },
      )
    }, 100);
  }

  if (!pendingFetch.ids.includes(id)) {
    pendingFetch.ids = pendingFetch.ids.concat([id]);

    dispatch({
      type: 'X/{{Hash $Type.LowerPlural "/FETCH_BEGIN"}}',
      payload: { id },
    });
  }
}

/** {{$Type.LowerPlural}}Fetch */
export function {{$Type.LowerPlural}}Fetch(id: string): (dispatch: (ev: any) => void) => void {
  return function(dispatch: (ev: any) => void): void {
    if (typeof id !== 'string') { throw new Error('{{$Type.LowerPlural}}Fetch: id must be a string'); }
    if (!id.match(/^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i)) { throw new Error('{{$Type.LowerPlural}}Fetch: id must be a uuid'); }

    batchFetch(id, dispatch);
  };
}

/** {{$Type.LowerPlural}}FetchIfRequired will only perform a fetch if the current results are older than the specified ttl, which is one minute by default */
export function {{$Type.LowerPlural}}FetchIfRequired(
  id: string,
  ttl: number = 1000 * 60,
  now: Date = new Date()
): (dispatch: (ev: any) => void, getState: () => { {{$Type.LowerPlural}}: State }) => void {
  return function(dispatch: (ev: any) => void, getState: () => { {{$Type.LowerPlural}}: State }) {
    const { {{$Type.LowerPlural}}: { fetchCache } } = getState();

    let refresh = false;

    const c = fetchCache[id];

    if (!c) {
      refresh = true;
    } else if (c.time) {
      if (!c.loading && now.valueOf() - c.time > ttl) {
        refresh = true;
      }
    } else {
      if (!c.loading) {
        refresh = true;
      }
    }

    if (refresh) {
      dispatch({{$Type.LowerPlural}}Fetch(id));
    }
  };
}

/** {{$Type.LowerPlural}}GetFetchMeta fetches the metadata related to a specific search query, if available */
export function {{$Type.LowerPlural}}GetFetchMeta(state: State, id: string): ?{ time: number, loading: number } {
  return state.fetchCache[id];
}

/** {{$Type.LowerPlural}}GetFetchLoading returns the loading status for a specific search query */
export function {{$Type.LowerPlural}}GetFetchLoading(state: State, id: string): boolean {
  const c = state.fetchCache[id];
  if (!c) {
    return false;
  }

  return c.loading > 0;
}

/** use{{$Type.Singular}}Fetch forms a react hook for a specific fetch query */
export function use{{$Type.Singular}}Fetch(id: ?string): {
  loading: boolean,
  record: ?{{$Type.Singular}},
} {
  const dispatch = useDispatch();
  useEffect(() => { if (id) { dispatch({{$Type.LowerPlural}}FetchIfRequired(id)); } });
  const { loading, record } = useSelector(({ {{$Type.LowerPlural}} }: { {{$Type.LowerPlural}}: State }) => ({
    loading: id ? {{$Type.LowerPlural}}GetFetchLoading({{$Type.LowerPlural}}, id) : false,
    record: id ? {{$Type.LowerPlural}}.{{$Type.LowerPlural}}.find(e => e.id === id) : null,
  }));

  const manager = useContext(SubscriptionsContext);
  useEffect(() => {
    if (!manager || !id) { return }
    manager.inc('{{$Type.Singular}}', id);

    return () => {
      if (!manager || !id) { return }
      manager.dec('{{$Type.Singular}}', id);
    }
  }, [manager, id]);

  return { loading, record };
}

{{if $Type.CanCreate}}
/** {{$Type.LowerPlural}}Create */
export function {{$Type.LowerPlural}}Create(
  input: {{$Type.Singular}}CreateInput,
  options?: {{$Type.Singular}}CreateOptions
): (dispatch: (ev: any) => void) => void {
  return function(dispatch: (ev: any) => void): void {
    dispatch({
      type: 'X/{{Hash $Type.LowerPlural "/CREATE_BEGIN"}}',
      payload: {},
    });

    axios.post('/api/{{$Type.LowerPlural}}', input).then(
      ({ data: { time, record, changed } }: {
        data: {
          time: string,
          record: {{$Type.Singular}},
          changed: { [key: string]: $ReadOnlyArray<any> },
        },
      }) => {
        dispatch({
          type: 'X/{{Hash $Type.LowerPlural "/CREATE_COMPLETE"}}',
          payload: { record, options: options || {} },
        });
        dispatch({
          type: 'X/RECORD_PUSH_MULTI',
          payload: { time: new Date(time).valueOf(), changed },
        });

        if (options && options.after) {
          setImmediate(options.after, null, record);
        }
      },
      (err: Error | { response: { data: ErrorResponse } }) => {
        dispatch({
          type: 'X/{{Hash $Type.LowerPlural "/CREATE_FAILED"}}',
          payload: { error: errorsEnsureError(err) },
        });

        if (options && options.after) {
          if (err && err.response && typeof err.response.data === 'object' && err.response.data !== null) {
            setImmediate(options.after, new Error(err.response.data.message));
          } else {
            setImmediate(options.after, err);
          }
        }
      }
    );
  };
}

/** {{$Type.LowerPlural}}CreateMultiple */
export function {{$Type.LowerPlural}}CreateMultiple(
  input: $ReadOnlyArray<{{$Type.Singular}}CreateInput>,
  options?: {{$Type.Singular}}CreateMultipleOptions
): (dispatch: (ev: any) => void) => void {
  return function(dispatch: (ev: any) => void): void {
    dispatch({
      type: 'X/{{Hash $Type.LowerPlural "/CREATE_MULTIPLE_BEGIN"}}',
      payload: { records: input, options: options || {} },
    });

    axios.post('/api/{{$Type.LowerPlural}}/_multi', { records: input }).then(
      ({ data: { time, records, changed } }: {
        data: {
          time: string,
          records: $ReadOnlyArray<{{$Type.Singular}}>,
          changed: { [key: string]: $ReadOnlyArray<any> },
        },
      }) => {
        dispatch({
          type: 'X/{{Hash $Type.LowerPlural "/CREATE_MULTIPLE_COMPLETE"}}',
          payload: { records, options: options || {} },
        });
        dispatch({
          type: 'X/RECORD_PUSH_MULTI',
          payload: { time: new Date(time).valueOf(), changed },
        });

        if (options && options.after) {
          setImmediate(options.after, null, records);
        }
      },
      (err: Error) => {
        dispatch({
          type: 'X/{{Hash $Type.LowerPlural "/CREATE_MULTIPLE_FAILED"}}',
          payload: { records: input, options: options || {}, error: errorsEnsureError(err) },
        });

        if (options && options.after) {
          setImmediate(options.after, err);
        }
      }
    );
  };
}
{{end}}

{{if $Type.CanUpdate}}
/** {{$Type.LowerPlural}}Update */
export function {{$Type.LowerPlural}}Update(
  input: {{$Type.Singular}},
  options?: {{$Type.Singular}}UpdateOptions
): (dispatch: (ev: any) => void, getState: () => ({ {{$Type.LowerPlural}}: State })) => void {
  return function(dispatch: (ev: any) => void, getState: () => ({ {{$Type.LowerPlural}}: State })) {
    const previous = getState().{{$Type.LowerPlural}}.{{$Type.LowerPlural}}.find(e => e.id === input.id);
    if (!previous) {
      return;
    }

    const timeoutHandle = getState().{{$Type.LowerPlural}}.timeouts[input.id];
    if (timeoutHandle) {
      clearTimeout(timeoutHandle);
      dispatch({ type: 'X/{{Hash $Type.LowerPlural "/UPDATE_CANCEL"}}', payload: { id: input.id } });
    }

    dispatch({
      type: 'X/{{Hash $Type.LowerPlural "/UPDATE_BEGIN"}}',
      payload: {
        record: input,
        timeout: setTimeout(
          () =>
            void axios.put('/api/{{$Type.LowerPlural}}/' + input.id, input).then(
              ({ data: { time, record, changed } }: {
                data: {
                  time: string,
                  record: {{$Type.Singular}},
                  changed: { [key: string]: $ReadOnlyArray<any> },
                },
              }) => {
                dispatch({
                  type: 'X/{{Hash $Type.LowerPlural "/UPDATE_COMPLETE"}}',
                  payload: { record, options: options || {} },
                });
                dispatch({
                  type: 'X/RECORD_PUSH_MULTI',
                  payload: { time: new Date(time).valueOf(), changed },
                });

                if (options && options.after) {
                  setImmediate(options.after, null, record);
                }
              },
              (err: Error | { response: { data: ErrorResponse } }) => {
                dispatch({
                  type: 'X/{{Hash $Type.LowerPlural "/UPDATE_FAILED"}}',
                  payload: { record: previous, error: errorsEnsureError(err) },
                });

                if (options && options.after) {
                  if (err && err.response && typeof err.response.data === 'object' && err.response.data !== null) {
                    setImmediate(options.after, new Error(err.response.data.message));
                  } else {
                    setImmediate(options.after, err);
                  }
                }
              }
            ),
          options && typeof options.timeout === 'number'
            ? options.timeout
            : 1000
        ),
      },
    });
  };
}

/** {{$Type.LowerPlural}}UpdateMultiple */
export function {{$Type.LowerPlural}}UpdateMultiple(
  input: $ReadOnlyArray<{{$Type.Singular}}>,
  options?: {{$Type.Singular}}UpdateMultipleOptions
): (dispatch: (ev: any) => void, getState: () => ({ {{$Type.LowerPlural}}: State })) => void {
  return function(dispatch: (ev: any) => void, getState: () => ({ {{$Type.LowerPlural}}: State })) {
    const {{$Type.LowerPlural}} = getState().{{$Type.LowerPlural}}.{{$Type.LowerPlural}};

    const previous = input.map(({ id }) => {{$Type.LowerPlural}}.find(e => e.id === id));
    if (!previous.length) {
      return;
    }

    const timeoutHandle = getState().{{$Type.LowerPlural}}.timeouts[input.map(e => e.id).sort().join(',')];
    if (timeoutHandle) {
      clearTimeout(timeoutHandle);
      dispatch({ type: 'X/{{Hash $Type.LowerPlural "/UPDATE_MULTIPLE_CANCEL"}}', payload: { ids: input.map(e => e.id).sort().join(',') } });
    }

    dispatch({
      type: 'X/{{Hash $Type.LowerPlural "/UPDATE_MULTIPLE_BEGIN"}}',
      payload: {
        records: input,
        timeout: setTimeout(
          () =>
            void axios.put('/api/{{$Type.LowerPlural}}/_multi', { records: input }).then(
              ({ data: { time, records, changed } }: {
                data: {
                  time: string,
                  records: $ReadOnlyArray<{{$Type.Singular}}>,
                  changed: { [key: string]: $ReadOnlyArray<any> },
                },
              }) => {
                dispatch({
                  type: 'X/{{Hash $Type.LowerPlural "/UPDATE_MULTIPLE_COMPLETE"}}',
                  payload: { records, options: options || {} },
                });
                dispatch({
                  type: 'X/RECORD_PUSH_MULTI',
                  payload: { time: new Date(time).valueOf(), changed },
                });

                if (options && options.after) {
                  setImmediate(options.after, null, records);
                }
              },
              (err: Error) => {
                dispatch({
                  type: 'X/{{Hash $Type.LowerPlural "/UPDATE_MULTIPLE_FAILED"}}',
                  payload: { record: previous, error: errorsEnsureError(err) },
                });

                if (options && options.after) {
                  setImmediate(options.after, err);
                }
              }
            ),
          options && typeof options.timeout === 'number'
            ? options.timeout
            : 1000
        ),
      },
    });
  };
}
{{end}}

/** {{$Type.LowerPlural}}Reset resets the whole {{$Type.Singular}} state */
export function {{$Type.LowerPlural}}Reset(): {
  type: 'X/{{Hash $Type.LowerPlural "/RESET"}}',
  payload: {},
} {
  return {
    type: 'X/{{Hash $Type.LowerPlural "/RESET"}}',
    payload: {},
  };
}

/** {{$Type.LowerPlural}}InvalidateCache invalidates the caches for {{$Type.Singular}} */
export function {{$Type.LowerPlural}}InvalidateCache(): {
  type: 'X/{{Hash $Type.LowerPlural "/INVALIDATE_CACHE"}}',
  payload: {},
} {
  return {
    type: 'X/{{Hash $Type.LowerPlural "/INVALIDATE_CACHE"}}',
    payload: {},
  };
}

const defaultState: State = {
  loading: 0,
  {{$Type.LowerPlural}}: [],
  searchCache: {},
  fetchCache: {},
  error: null,
  timeouts: {},
};

export default function reducer(state: State = defaultState, action: Action): State {
  switch (action.type) {
    case 'X/{{Hash $Type.LowerPlural "/SEARCH_BEGIN"}}': {
      const { params, key, page } = action.payload;

      return {
        ...state,
        loading: state.loading + 1,
        searchCache: updateSearchCacheLoading(state.searchCache, params, key, page, 1),
      };
    }
    case 'X/{{Hash $Type.LowerPlural "/SEARCH_COMPLETE"}}': {
      const { params, key, time, total, page, records } = action.payload;

      const ids = records.map((e) => e.id);

      return {
        ...state,
        loading: state.loading - 1,
        error: null,
        {{$Type.LowerPlural}}: mergeArrays(state.{{$Type.LowerPlural}}, records),
        searchCache: updateSearchCacheComplete(state.searchCache, params, key, page, time, total, ids),
        fetchCache: updateFetchCachePushMulti(state.fetchCache, ids, time),
      };
    }
    case 'X/{{Hash $Type.LowerPlural "/SEARCH_FAILED"}}': {
      const { params, key, page, time, error } = action.payload;

      return {
        ...state,
        loading: state.loading - 1,
        error: error,
        searchCache: updateSearchCacheError(state.searchCache, params, key, page, time, error),
      };
    }
    case 'X/{{Hash $Type.LowerPlural "/FETCH_BEGIN"}}': {
      const { id } = action.payload;

      return {
        ...state,
        loading: state.loading + 1,
        fetchCache: updateFetchCacheLoading(state.fetchCache, id, 1),
      };
    }
    case 'X/{{Hash $Type.LowerPlural "/FETCH_COMPLETE_MULTI"}}': {
      const { ids, time, records } = action.payload;

      return {
        ...state,
        loading: state.loading - ids.length,
        error: null,
        {{$Type.LowerPlural}}: mergeArrays(state.{{$Type.LowerPlural}}, records),
        fetchCache: updateFetchCacheCompleteMulti(state.fetchCache, ids, time),
      };
    }
    case 'X/{{Hash $Type.LowerPlural "/FETCH_FAILED_MULTI"}}': {
      const { ids, time, error } = action.payload;

      return {
        ...state,
        loading: state.loading - ids.length,
        error: error,
        fetchCache: updateFetchCacheErrorMulti(state.fetchCache, ids, time, error),
      };
    }
{{if $Type.CanCreate}}
    case 'X/{{Hash $Type.LowerPlural "/CREATE_BEGIN"}}':
      return {
        ...state,
        loading: state.loading + 1,
      };
    case 'X/{{Hash $Type.LowerPlural "/CREATE_COMPLETE"}}': {
      const { record, options } = action.payload;

      return {
        ...state,
        loading: options.push ? state.loading : state.loading - 1,
        error: null,
        searchCache: typeof options.invalidate === 'function' ? options.invalidate(state.searchCache) : options.invalidate === true ? {} : state.searchCache,
        {{$Type.LowerPlural}}: mergeArrays(state.{{$Type.LowerPlural}}, [ record ]),
      };
    }
    case 'X/{{Hash $Type.LowerPlural "/CREATE_FAILED"}}':
      return {
        ...state,
        loading: state.loading - 1,
        error: action.payload.error,
      };
    case 'X/{{Hash $Type.LowerPlural "/CREATE_MULTIPLE_BEGIN"}}': {
      const { records, options } = action.payload;

      return {
        ...state,
        loading: state.loading + 1,
        searchCache: typeof options.invalidate === 'function' ? options.invalidate(state.searchCache) : options.invalidate === true ? {} : state.searchCache,
        {{$Type.LowerPlural}}: mergeArrays(state.{{$Type.LowerPlural}}, records),
      };
    }
    case 'X/{{Hash $Type.LowerPlural "/CREATE_MULTIPLE_COMPLETE"}}': {
      const { records, options } = action.payload;

      return {
        ...state,
        loading: state.loading - 1,
        error: null,
        searchCache: typeof options.invalidate === 'function' ? options.invalidate(state.searchCache) : options.invalidate === true ? {} : state.searchCache,
        {{$Type.LowerPlural}}: mergeArrays(state.{{$Type.LowerPlural}}, records),
      };
    }
    case 'X/{{Hash $Type.LowerPlural "/CREATE_MULTIPLE_FAILED"}}': {
      const { records, options, error } = action.payload;
      const ids = records.map((e) => e.id);

      return {
        ...state,
        loading: state.loading - 1,
        error: error,
        searchCache: typeof options.invalidate === 'function' ? options.invalidate(state.searchCache) : options.invalidate === true ? {} : state.searchCache,
        {{$Type.LowerPlural}}: state.{{$Type.LowerPlural}}.filter((e) => ids.indexOf(e.id) === -1),
      };
    }
{{end}}
{{if $Type.CanUpdate}}
    case 'X/{{Hash $Type.LowerPlural "/UPDATE_BEGIN"}}':
      return {
        ...state,
        loading: state.loading + 1,
        {{$Type.LowerPlural}}: mergeArrays(state.{{$Type.LowerPlural}}, [
          action.payload.record,
        ]),
        timeouts: {
          ...state.timeouts,
          [action.payload.record.id]: action.payload.timeout,
        },
      };
    case 'X/{{Hash $Type.LowerPlural "/UPDATE_CANCEL"}}':
      return {
        ...state,
        loading: state.loading - 1,
        timeouts: {
          ...state.timeouts,
          [action.payload.id]: null,
        },
      };
    case 'X/{{Hash $Type.LowerPlural "/UPDATE_COMPLETE"}}': {
      const { record, options } = action.payload;

      return {
        ...state,
        loading: options.push ? state.loading : state.loading - 1,
        error: null,
        searchCache: typeof options.invalidate === 'function' ? options.invalidate(state.searchCache) : options.invalidate === true ? {} : state.searchCache,
        {{$Type.LowerPlural}}: mergeArrays(state.{{$Type.LowerPlural}}, [ record ]),
        timeouts: {
          ...state.timeouts,
          [action.payload.record.id]: null,
        },
      };
    }
    case 'X/{{Hash $Type.LowerPlural "/UPDATE_FAILED"}}':
      return {
        ...state,
        loading: state.loading - 1,
        error: action.payload.error,
        {{$Type.LowerPlural}}: mergeArrays(state.{{$Type.LowerPlural}}, [
          action.payload.record,
        ]),
        timeouts: {
          ...state.timeouts,
          [action.payload.record.id]: null,
        },
      };
    case 'X/{{Hash $Type.LowerPlural "/UPDATE_MULTIPLE_BEGIN"}}': {
      const { records, timeout } = action.payload;

      return {
        ...state,
        loading: state.loading + 1,
        {{$Type.LowerPlural}}: mergeArrays(state.{{$Type.LowerPlural}}, records),
        timeouts: {
          ...state.timeouts,
          [records.map(e => e.id).sort().join(',')]: timeout,
        },
      };
    }
    case 'X/{{Hash $Type.LowerPlural "/UPDATE_MULTIPLE_CANCEL"}}': {
      const { ids } = action.payload;

      return {
        ...state,
        loading: state.loading - 1,
        timeouts: {
          ...state.timeouts,
          [ids]: null,
        },
      };
    }
    case 'X/{{Hash $Type.LowerPlural "/UPDATE_MULTIPLE_COMPLETE"}}': {
      const { records, options } = action.payload;

      return {
        ...state,
        loading: options.push ? state.loading : state.loading - 1,
        error: null,
        searchCache: typeof options.invalidate === 'function' ? options.invalidate(state.searchCache) : options.invalidate === true ? {} : state.searchCache,
        {{$Type.LowerPlural}}: mergeArrays(state.{{$Type.LowerPlural}}, records),
        timeouts: {
          ...state.timeouts,
          [records.map(e => e.id).sort().join(',')]: null,
        },
      };
    }
    case 'X/{{Hash $Type.LowerPlural "/UPDATE_MULTIPLE_FAILED"}}': {
      const { records, error } = action.payload;

      return {
        ...state,
        loading: state.loading - 1,
        error: error,
        {{$Type.LowerPlural}}: mergeArrays(state.{{$Type.LowerPlural}}, records),
        timeouts: {
          ...state.timeouts,
          [records.map(e => e.id).sort().join(',')]: null,
        },
      };
    }
{{end}}
    case 'X/{{Hash $Type.LowerPlural "/RECORD_PUSH"}}': {
      const { time, record } = action.payload;

      return {
        ...state,
        {{$Type.LowerPlural}}: mergeArrays(state.{{$Type.LowerPlural}}, [record]),
        fetchCache: updateFetchCachePushMulti(state.fetchCache, [record.id], time),
      };
    }
    case 'X/{{Hash $Type.LowerPlural "/RECORD_PUSH_MULTI"}}': {
      const { time, records } = action.payload;

      return {
        ...state,
        {{$Type.LowerPlural}}: mergeArrays(state.{{$Type.LowerPlural}}, records),
        fetchCache: updateFetchCachePushMulti(state.fetchCache, records.map(e => e.id), time),
      };
    }
    case 'X/{{Hash $Type.LowerPlural "/INVALIDATE_CACHE"}}':
      return { ...state, searchCache: {}, fetchCache: {} };
    case 'X/{{Hash $Type.LowerPlural "/RESET"}}':
      return defaultState;
    case 'X/INVALIDATE': {
      const ids = action.payload.{{$Type.Singular}};

      if (!ids) {
        return state;
      }

      return {
        ...state,
        fetchCache: invalidateFetchCacheWithIDs(state.fetchCache, ids),
        searchCache: invalidateSearchCacheWithIDs(state.searchCache, ids),
      };
    }
{{if $Type.HasVersion}}
    case 'X/INVALIDATE_OUTDATED': {
      const pairs = action.payload.{{$Type.Singular}};

      if (!pairs) {
        return state;
      }

      const ids = pairs.filter(([id, version]) => {
        const v = state.{{$Type.LowerPlural}}.find(e => e.id === id);
        return v && v.version < version;
      }).map(([id]) => id);

      if (ids.length === 0) {
        return state;
      }

      return {
        ...state,
        fetchCache: invalidateFetchCacheWithIDs(state.fetchCache, ids),
        searchCache: invalidateSearchCacheWithIDs(state.searchCache, ids),
      };
    }
{{end}}
    case 'X/RECORD_PUSH_MULTI': {
      const { time, changed } = action.payload;

      const records = changed.{{$Type.Singular}};
      if (!records) {
        return state;
      }

      return {
        ...state,
        {{$Type.LowerPlural}}: mergeArrays(state.{{$Type.LowerPlural}}, records),
        fetchCache: updateFetchCachePushMulti(state.fetchCache, records.map(e => e.id), time),
      };
    }
    default:
      return state;
  }
}
`))
