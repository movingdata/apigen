package main

type JSGenerator struct {
  dir string
}

func NewJSGenerator(dir string) *JSGenerator {
  return &JSGenerator{dir: dir}
}

func (g *JSGenerator) Name() string {
  return "js"
}

func (g *JSGenerator) Model(model *Model) []writer {
  return []writer{
    &basicWriter{
      name:     "individual",
      language: "js",
      file:     g.dir + "/ducks/" + model.LowerPlural + ".js",
      write:    templateWriter(jsTemplate, map[string]interface{}{"Model": model}),
    },
  }
}

var jsTemplate = `
{{$Model := .Model}}

// @flow

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

{{range $Field := $Model.Fields}}
{{if $Field.Enum}}
export type {{$Model.Singular}}{{$Field.GoName}} =
{{- range $Enum := $Field.Enum}}
  | "{{$Enum.Value}}"
{{- end}}

{{range $Enum := $Field.Enum}}
export const {{$Model.LowerPlural}}Enum{{$Field.GoName}}{{$Enum.GoName}} = '{{$Enum.Value}}';
{{- end}}

export const {{$Model.LowerPlural}}Values{{$Field.GoName}}: $ReadOnlyArray<{{$Model.Singular}}{{$Field.GoName}}> = [
{{- range $Enum := $Field.Enum}}
  {{$Model.LowerPlural}}Enum{{$Field.GoName}}{{$Enum.GoName}},
{{- end}}
];

export const {{$Model.LowerPlural}}Labels{{$Field.GoName}}: { [key: {{$Model.Singular}}{{$Field.GoName}}]: string } = {
{{- range $Enum := $Field.Enum}}
  [{{$Model.LowerPlural}}Enum{{$Field.GoName}}{{$Enum.GoName}}]: '{{$Enum.Label}}',
{{- end}}
}
{{- end}}
{{- end}}

const defaultPageSize = 10;

/** {{$Model.Singular}} is a complete {{$Model.Singular}} object */
export type {{$Model.Singular}} = {|
{{- range $Field := $Model.Fields}}
  {{$Field.APIName}}: {{$Field.JSType}},
{{- end}}
|};

{{if $Model.HasAPICreate}}
/** {{$Model.Singular}}CreateInput is the data needed to call {{$Model.LowerPlural}}Create */
export type {{$Model.Singular}}CreateInput = {|
  id: {{$Model.IDField.JSType}},
{{- range $Field := $Model.Fields}}
{{- if not $Field.IgnoreCreate }}
  {{$Field.APIName}}{{if $Field.OmitEmpty}}?{{end}}: {{$Field.JSType}},
{{- end}}
{{- end}}
|};
{{end}}

/** {{$Model.Singular}}SearchParams is used to call {{$Model.LowerPlural}}Search */
export type {{$Model.Singular}}SearchParams = {|
{{- range $Field := $Model.Fields}}
{{- range $Filter := $Field.Filters}}
  {{$Filter.Name}}?: {{$Filter.JSType}},
{{- end}}
{{- end}}
{{- range $Filter := $Model.SpecialFilters}}
  {{$Filter.Name}}?: {{$Filter.JSType}},
{{- end}}
  order?: string,
  pageSize?: number,
  page: SearchPageKey,
|};

export type State = {
  loading: number,
  {{$Model.LowerPlural}}: $ReadOnlyArray<{{$Model.Singular}}>,
  error: ?ErrorResponse,
  searchCache: SearchCache<{{$Model.Singular}}SearchParams>,
  fetchCache: FetchCache,
  timeouts: { [key: string]: ?TimeoutID },
};

{{if (or $Model.HasAPICreate $Model.HasAPIUpdate)}}
type Invalidator = (c: SearchCache<{{$Model.Singular}}SearchParams>) => SearchCache<{{$Model.Singular}}SearchParams>;
{{end}}

{{if $Model.HasAPICreate}}
type {{$Model.Singular}}CreateOptions = {
  invalidate?: boolean | Invalidator,
  after?: (err: ?Error, record?: {{$Model.Singular}}) => void,
  push?: boolean,
};

type {{$Model.Singular}}CreateMultipleOptions = {
  invalidate?: boolean | Invalidator,
  after?: (err: ?Error, records?: $ReadOnlyArray<{{$Model.Singular}}>) => void,
  push?: boolean,
  timeout?: number,
};
{{end}}

{{if $Model.HasAPIUpdate}}
type {{$Model.Singular}}UpdateOptions = {
  invalidate?: boolean | Invalidator,
  after?: (err: ?Error, record?: {{$Model.Singular}}) => void,
  push?: boolean,
  timeout?: number,
};

type {{$Model.Singular}}UpdateMultipleOptions = {
  invalidate?: boolean | Invalidator,
  after?: (err: ?Error, records?: $ReadOnlyArray<{{$Model.Singular}}>) => void,
  push?: boolean,
  timeout?: number,
};
{{end}}

export const actionCreateBegin = 'X/{{Hash $Model.LowerPlural "/CREATE_BEGIN"}}';
export const actionCreateComplete = 'X/{{Hash $Model.LowerPlural "/CREATE_COMPLETE"}}';
export const actionCreateFailed = 'X/{{Hash $Model.LowerPlural "/CREATE_FAILED"}}';
export const actionCreateMultipleBegin = 'X/{{Hash $Model.LowerPlural "/CREATE_MULTIPLE_BEGIN"}}';
export const actionCreateMultipleComplete = 'X/{{Hash $Model.LowerPlural "/CREATE_MULTIPLE_COMPLETE"}}';
export const actionCreateMultipleFailed = 'X/{{Hash $Model.LowerPlural "/CREATE_MULTIPLE_FAILED"}}';
export const actionFetchBegin = 'X/{{Hash $Model.LowerPlural "/FETCH_BEGIN"}}';
export const actionFetchCompleteMulti = 'X/{{Hash $Model.LowerPlural "/FETCH_COMPLETE_MULTI"}}';
export const actionFetchFailedMulti = 'X/{{Hash $Model.LowerPlural "/FETCH_FAILED_MULTI"}}';
export const actionReset = 'X/{{Hash $Model.LowerPlural "/RESET"}}';
export const actionSearchBegin = 'X/{{Hash $Model.LowerPlural "/SEARCH_BEGIN"}}';
export const actionSearchComplete = 'X/{{Hash $Model.LowerPlural "/SEARCH_COMPLETE"}}';
export const actionSearchFailed = 'X/{{Hash $Model.LowerPlural "/SEARCH_FAILED"}}';
export const actionUpdateBegin = 'X/{{Hash $Model.LowerPlural "/UPDATE_BEGIN"}}';
export const actionUpdateCancel = 'X/{{Hash $Model.LowerPlural "/UPDATE_CANCEL"}}';
export const actionUpdateComplete = 'X/{{Hash $Model.LowerPlural "/UPDATE_COMPLETE"}}';
export const actionUpdateFailed = 'X/{{Hash $Model.LowerPlural "/UPDATE_FAILED"}}';
export const actionUpdateMultipleBegin = 'X/{{Hash $Model.LowerPlural "/UPDATE_MULTIPLE_BEGIN"}}';
export const actionUpdateMultipleCancel = 'X/{{Hash $Model.LowerPlural "/UPDATE_MULTIPLE_CANCEL"}}';
export const actionUpdateMultipleComplete = 'X/{{Hash $Model.LowerPlural "/UPDATE_MULTIPLE_COMPLETE"}}';
export const actionUpdateMultipleFailed = 'X/{{Hash $Model.LowerPlural "/UPDATE_MULTIPLE_FAILED"}}';
export const actionInvalidateCache = 'X/{{Hash $Model.LowerPlural "/INVALIDATE_CACHE"}}';
export const actionRecordPush = 'X/{{Hash $Model.LowerPlural "/RECORD_PUSH"}}';
export const actionRecordPushMulti = 'X/{{Hash $Model.LowerPlural "/RECORD_PUSH_MULTI"}}';

export type Action =
  | {
      type: 'X/{{Hash $Model.LowerPlural "/SEARCH_BEGIN"}}',
      payload: { params: {{$Model.Singular}}SearchParams, key: string, page: SearchPageKey },
    }
  | {
      type: 'X/{{Hash $Model.LowerPlural "/SEARCH_COMPLETE"}}',
      payload: {
        records: $ReadOnlyArray<{{$Model.Singular}}>,
        total: number,
        time: number,
        params: {{$Model.Singular}}SearchParams,
        key: string,
        page: SearchPageKey,
      },
    }
  | {
      type: 'X/{{Hash $Model.LowerPlural "/SEARCH_FAILED"}}',
      payload: {
        time: number,
        params: {{$Model.Singular}}SearchParams,
        key: string,
        page: SearchPageKey,
        error: ErrorResponse,
      },
    }
  | { type: 'X/{{Hash $Model.LowerPlural "/FETCH_BEGIN"}}', payload: { id: {{if (EqualStrings $Model.IDField.JSType "number")}}number{{else}}string{{end}} } }
  | {
      type: 'X/{{Hash $Model.LowerPlural "/FETCH_COMPLETE_MULTI"}}',
      payload: { ids: $ReadOnlyArray<string>, time: number, records: $ReadOnlyArray<{{$Model.Singular}}> },
    }
  | {
      type: 'X/{{Hash $Model.LowerPlural "/FETCH_FAILED_MULTI"}}',
      payload: { ids: $ReadOnlyArray<string>, time: number, error: ErrorResponse },
    }
{{if $Model.HasAPICreate}}
  | {
      type: 'X/{{Hash $Model.LowerPlural "/CREATE_BEGIN"}}',
      payload: { record: {{$Model.Singular}} },
    }
  | {
      type: 'X/{{Hash $Model.LowerPlural "/CREATE_COMPLETE"}}',
      payload: { record: {{$Model.Singular}}, options: {{$Model.Singular}}CreateOptions },
    }
  | {
      type: 'X/{{Hash $Model.LowerPlural "/CREATE_FAILED"}}',
      payload: { error: ErrorResponse },
    }
  | {
      type: 'X/{{Hash $Model.LowerPlural "/CREATE_MULTIPLE_BEGIN"}}',
      payload: { records: $ReadOnlyArray<{{$Model.Singular}}>, options: {{$Model.Singular}}CreateMultipleOptions },
    }
  | {
      type: 'X/{{Hash $Model.LowerPlural "/CREATE_MULTIPLE_COMPLETE"}}',
      payload: { records: $ReadOnlyArray<{{$Model.Singular}}>, options: {{$Model.Singular}}CreateMultipleOptions },
    }
  | {
      type: 'X/{{Hash $Model.LowerPlural "/CREATE_MULTIPLE_FAILED"}}',
      payload: { records: $ReadOnlyArray<{{$Model.Singular}}>, options: {{$Model.Singular}}CreateMultipleOptions, error: ErrorResponse },
    }
{{end}}
{{if $Model.HasAPIUpdate}}
  | {
      type: 'X/{{Hash $Model.LowerPlural "/UPDATE_BEGIN"}}',
      payload: { record: {{$Model.Singular}}, timeout: number },
    }
  | { type: 'X/{{Hash $Model.LowerPlural "/UPDATE_CANCEL"}}', payload: { id: {{if (EqualStrings $Model.IDField.JSType "number")}}number{{else}}string{{end}} } }
  | {
      type: 'X/{{Hash $Model.LowerPlural "/UPDATE_COMPLETE"}}',
      payload: { record: {{$Model.Singular}}, options: {{$Model.Singular}}UpdateOptions },
    }
  | {
      type: 'X/{{Hash $Model.LowerPlural "/UPDATE_FAILED"}}',
      payload: { record: {{$Model.Singular}}, error: ErrorResponse },
    }
  | {
      type: 'X/{{Hash $Model.LowerPlural "/UPDATE_MULTIPLE_BEGIN"}}',
      payload: { records: $ReadOnlyArray<{{$Model.Singular}}>, timeout: number },
    }
  | { type: 'X/{{Hash $Model.LowerPlural "/UPDATE_MULTIPLE_CANCEL"}}', payload: { ids: string } }
  | {
      type: 'X/{{Hash $Model.LowerPlural "/UPDATE_MULTIPLE_COMPLETE"}}',
      payload: { records: $ReadOnlyArray<{{$Model.Singular}}>, options: {{$Model.Singular}}UpdateMultipleOptions },
    }
  | {
      type: 'X/{{Hash $Model.LowerPlural "/UPDATE_MULTIPLE_FAILED"}}',
      payload: { records: $ReadOnlyArray<{{$Model.Singular}}>, error: ErrorResponse },
    }
{{end}}
  | { type: 'X/{{Hash $Model.LowerPlural "/RESET"}}', payload: {} }
  | { type: 'X/{{Hash $Model.LowerPlural "/INVALIDATE_CACHE"}}', payload: {} }
  | { type: 'X/{{Hash $Model.LowerPlural "/RECORD_PUSH"}}', payload: { time: number, record: {{$Model.Singular}} } }
  | { type: 'X/{{Hash $Model.LowerPlural "/RECORD_PUSH_MULTI"}}', payload: { time: number, records: $ReadOnlyArray<{{$Model.Singular}}> } }
  | { type: 'X/INVALIDATE', payload: { {{$Model.Singular}}?: $ReadOnlyArray<string> } }
{{if $Model.HasVersion}}
  | { type: 'X/INVALIDATE_OUTDATED', payload: { {{$Model.Singular}}?: $ReadOnlyArray<[string, number]> } }
{{end}}
  | { type: 'X/RECORD_PUSH_MULTI', payload: { time: number, changed: { {{$Model.Singular}}?: $ReadOnlyArray<{{$Model.Singular}}> } } };

/** {{$Model.LowerPlural}}Search */
export function {{$Model.LowerPlural}}Search(params: {{$Model.Singular}}SearchParams): (dispatch: (ev: any) => void) => void {
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
      type: 'X/{{Hash $Model.LowerPlural "/SEARCH_BEGIN"}}',
      payload: { params, key, page: params.page },
    });

    axios.get('/api/{{$Model.LowerPlural}}?' + p.toString()).then(
      ({ data: { records, total, time } }: {
        data: { records: $ReadOnlyArray<{{$Model.Singular}}>, total: number, time: string },
      }) => void dispatch({
        type: 'X/{{Hash $Model.LowerPlural "/SEARCH_COMPLETE"}}',
        payload: { records, total, time: new Date(time).valueOf(), params, key, page: params.page },
      }),
      (err: Error) => {
        dispatch({
          type: 'X/{{Hash $Model.LowerPlural "/SEARCH_FAILED"}}',
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

/** {{$Model.LowerPlural}}SearchIfRequired will only perform a search if the current results are older than the specified ttl, which is one minute by default */
export function {{$Model.LowerPlural}}SearchIfRequired(
  params: {{$Model.Singular}}SearchParams,
  ttl: number = 1000 * 60,
  now: Date = new Date()
): (dispatch: (ev: any) => void, getState: () => { {{$Model.LowerPlural}}: State }) => void {
  return function(dispatch: (ev: any) => void, getState: () => { {{$Model.LowerPlural}}: State }): void {
    const { {{$Model.LowerPlural}}: { searchCache } } = getState();

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
      dispatch({{$Model.LowerPlural}}Search(params));
    }
  };
}

/** {{$Model.LowerPlural}}GetSearchRecords fetches the {{$Model.Singular}} objects related to a specific search query, if available */
export function {{$Model.LowerPlural}}GetSearchRecords(
  state: State,
  params: {{$Model.Singular}}SearchParams
): ?$ReadOnlyArray<{{$Model.Singular}}> {
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
    state.{{$Model.LowerPlural}}.find(e => String(e.id) === String(id))
  ).reduce((arr, e) => e ? [ ...arr, e ] : arr, ([]: $ReadOnlyArray<{{$Model.Singular}}>));
}

/** {{$Model.LowerPlural}}GetSearchMeta fetches the metadata related to a specific search query, if available */
export function {{$Model.LowerPlural}}GetSearchMeta(
  state: State,
  params: {{$Model.Singular}}SearchParams
): ?{ time: number, total: number, loading: number } {
  const k = makeSearchKey(params);

  const c = state.searchCache[k];
  if (!c || !c.pages) {
    return null;
  }

  const p = c.pages[String(params.page)];

  return { time: c.time, total: c.total, loading: p ? p.loading : 0 };
}

/** {{$Model.LowerPlural}}GetSearchLoading returns the loading status for a specific search query */
export function {{$Model.LowerPlural}}GetSearchLoading(
  state: State,
  params: {{$Model.Singular}}SearchParams
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

export type {{$Model.Singular}}SearchModifier = (params: {{$Model.Singular}}SearchParams) => {{$Model.Singular}}SearchParams;

/** use{{$Model.Singular}}Search forms a react hook for a specific search query */
export function use{{$Model.Singular}}Search(params: {{$Model.Singular}}SearchParams, ...modifiers: Array<{{$Model.Singular}}SearchModifier>): {
  meta: ?{ time: number, total: number, loading: number },
  loading: boolean,
  records: $ReadOnlyArray<{{$Model.Singular}}>,
} {
  const modified = modifiers.reduce((p, fn) => fn(p), params);

  const dispatch = useDispatch();
  useEffect(() => void dispatch({{$Model.LowerPlural}}SearchIfRequired(modified)));
  const { meta, loading, records } = useSelector(({ {{$Model.LowerPlural}} }: { {{$Model.LowerPlural}}: State }) => ({
    meta: {{$Model.LowerPlural}}GetSearchMeta({{$Model.LowerPlural}}, modified),
    loading: {{$Model.LowerPlural}}GetSearchLoading({{$Model.LowerPlural}}, modified) || !{{$Model.LowerPlural}}GetSearchMeta({{$Model.LowerPlural}}, modified),
    records: {{$Model.LowerPlural}}GetSearchRecords({{$Model.LowerPlural}}, modified) || [],
  }));

  const manager = useContext(SubscriptionsContext);
  const ids = records.map(e => e.id).sort().join(',');
  useEffect(() => {
    if (!manager || !ids) { return }
    ids.split(',').forEach(id => manager.inc('{{$Model.Singular}}', id));

    return () => {
      if (!manager || !ids) { return }
      ids.split(',').forEach(id => manager.dec('{{$Model.Singular}}', id));
    }
  }, [manager, ids]);

  return { meta, loading, records };
}

/** pendingFetch is a module-level metadata cache for ongoing fetch operations */ 
const pendingFetch: {
  timeout: ?TimeoutID,
  ids: $ReadOnlyArray<{{if (EqualStrings $Model.IDField.JSType "number")}}number{{else}}string{{end}}>,
} = {
  timeout: null,
  ids: [],
};

function batchFetch(id: {{if (EqualStrings $Model.IDField.JSType "number")}}number{{else}}string{{end}}, dispatch: (ev: any) => void) {
  if (pendingFetch.timeout === null) {
    pendingFetch.timeout = setTimeout(() => {
      const { ids } = pendingFetch;

      pendingFetch.timeout = null;
      pendingFetch.ids = [];

      axios.get('/api/{{$Model.LowerPlural}}?idIn=' + ids.join(',')).then(
        ({ data: { records }, }: { data: { records: $ReadOnlyArray<{{$Model.Singular}}> } }) => {
          dispatch({
            type: 'X/{{Hash $Model.LowerPlural "/FETCH_COMPLETE_MULTI"}}',
            payload: { ids, time: Date.now(), records },
          });
        },
        (err) => {
          dispatch({
            type: 'X/{{Hash $Model.LowerPlural "/FETCH_FAILED_MULTI"}}',
            payload: { ids, time: Date.now(), error: errorsEnsureError(err) },
          });
        },
      )
    }, 100);
  }

  if (!pendingFetch.ids.includes(id)) {
    pendingFetch.ids = pendingFetch.ids.concat([id]);

    dispatch({
      type: 'X/{{Hash $Model.LowerPlural "/FETCH_BEGIN"}}',
      payload: { id },
    });
  }
}

/** {{$Model.LowerPlural}}Fetch */
export function {{$Model.LowerPlural}}Fetch(id: {{if (EqualStrings $Model.IDField.JSType "number")}}number{{else}}string{{end}}): (dispatch: (ev: any) => void) => void {
  return function(dispatch: (ev: any) => void): void {
{{if (EqualStrings $Model.IDField.JSType "number")}}
    if (typeof id !== 'number') { throw new Error('{{$Model.LowerPlural}}Fetch: id must be a number'); }
    if (Number.isNaN(id)) { throw new Error('{{$Model.LowerPlural}}Fetch: id can not be NaN'); }
    if (id < 0) { throw new Error('{{$Model.LowerPlural}}Fetch: id must be zero or greater'); }
{{else}}
    if (typeof id !== 'string') { throw new Error('{{$Model.LowerPlural}}Fetch: id must be a string'); }
    if (!id.match(/^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i)) { throw new Error('{{$Model.LowerPlural}}Fetch: id must be a uuid'); }
{{end}}

    batchFetch(id, dispatch);
  };
}

/** {{$Model.LowerPlural}}FetchIfRequired will only perform a fetch if the current results are older than the specified ttl, which is one minute by default */
export function {{$Model.LowerPlural}}FetchIfRequired(
  id: {{if (EqualStrings $Model.IDField.JSType "number")}}number{{else}}string{{end}},
  ttl: number = 1000 * 60,
  now: Date = new Date()
): (dispatch: (ev: any) => void, getState: () => { {{$Model.LowerPlural}}: State }) => void {
  return function(dispatch: (ev: any) => void, getState: () => { {{$Model.LowerPlural}}: State }) {
    const { {{$Model.LowerPlural}}: { fetchCache } } = getState();

    let refresh = false;

    const c = fetchCache[String(id)];

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
      dispatch({{$Model.LowerPlural}}Fetch(id));
    }
  };
}

/** {{$Model.LowerPlural}}GetFetchMeta fetches the metadata related to a specific search query, if available */
export function {{$Model.LowerPlural}}GetFetchMeta(state: State, id: {{if (EqualStrings $Model.IDField.JSType "number")}}number{{else}}string{{end}}): ?{ time: number, loading: number } {
  return state.fetchCache[String(id)];
}

/** {{$Model.LowerPlural}}GetFetchLoading returns the loading status for a specific search query */
export function {{$Model.LowerPlural}}GetFetchLoading(state: State, id: {{if (EqualStrings $Model.IDField.JSType "number")}}number{{else}}string{{end}}): boolean {
  const c = state.fetchCache[String(id)];
  if (!c) {
    return false;
  }

  return c.loading > 0;
}

/** use{{$Model.Singular}}Fetch forms a react hook for a specific fetch query */
export function use{{$Model.Singular}}Fetch(id: ?{{if (EqualStrings $Model.IDField.JSType "number")}}number{{else}}string{{end}}): {
  loading: boolean,
  record: ?{{$Model.Singular}},
} {
  const dispatch = useDispatch();
  useEffect(() => { if (id) { dispatch({{$Model.LowerPlural}}FetchIfRequired(id)); } });
  const { loading, record } = useSelector(({ {{$Model.LowerPlural}} }: { {{$Model.LowerPlural}}: State }) => ({
    loading: id ? {{$Model.LowerPlural}}GetFetchLoading({{$Model.LowerPlural}}, id) : false,
    record: id ? {{$Model.LowerPlural}}.{{$Model.LowerPlural}}.find(e => String(e.id) === String(id)) : null,
  }));

  const manager = useContext(SubscriptionsContext);
  useEffect(() => {
    if (!manager || !id) { return }
    manager.inc('{{$Model.Singular}}', id);

    return () => {
      if (!manager || !id) { return }
      manager.dec('{{$Model.Singular}}', id);
    }
  }, [manager, id]);

  return { loading, record };
}

{{if $Model.HasAPICreate}}
/** {{$Model.LowerPlural}}Create */
export function {{$Model.LowerPlural}}Create(
  input: {{$Model.Singular}}CreateInput,
  options?: {{$Model.Singular}}CreateOptions
): (dispatch: (ev: any) => void) => void {
  return function(dispatch: (ev: any) => void): void {
    dispatch({
      type: 'X/{{Hash $Model.LowerPlural "/CREATE_BEGIN"}}',
      payload: {},
    });

    axios.post('/api/{{$Model.LowerPlural}}', input).then(
      ({ data: { time, record, changed } }: {
        data: {
          time: string,
          record: {{$Model.Singular}},
          changed: { [key: string]: $ReadOnlyArray<any> },
        },
      }) => {
        dispatch({
          type: 'X/{{Hash $Model.LowerPlural "/CREATE_COMPLETE"}}',
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
          type: 'X/{{Hash $Model.LowerPlural "/CREATE_FAILED"}}',
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

/** {{$Model.LowerPlural}}CreateMultiple */
export function {{$Model.LowerPlural}}CreateMultiple(
  input: $ReadOnlyArray<{{$Model.Singular}}CreateInput>,
  options?: {{$Model.Singular}}CreateMultipleOptions
): (dispatch: (ev: any) => void) => void {
  return function(dispatch: (ev: any) => void): void {
    dispatch({
      type: 'X/{{Hash $Model.LowerPlural "/CREATE_MULTIPLE_BEGIN"}}',
      payload: { records: input, options: options || {} },
    });

    axios.post('/api/{{$Model.LowerPlural}}/_multi', { records: input }).then(
      ({ data: { time, records, changed } }: {
        data: {
          time: string,
          records: $ReadOnlyArray<{{$Model.Singular}}>,
          changed: { [key: string]: $ReadOnlyArray<any> },
        },
      }) => {
        dispatch({
          type: 'X/{{Hash $Model.LowerPlural "/CREATE_MULTIPLE_COMPLETE"}}',
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
          type: 'X/{{Hash $Model.LowerPlural "/CREATE_MULTIPLE_FAILED"}}',
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

{{if $Model.HasAPIUpdate}}
/** {{$Model.LowerPlural}}Update */
export function {{$Model.LowerPlural}}Update(
  input: {{$Model.Singular}},
  options?: {{$Model.Singular}}UpdateOptions
): (dispatch: (ev: any) => void, getState: () => ({ {{$Model.LowerPlural}}: State })) => void {
  return function(dispatch: (ev: any) => void, getState: () => ({ {{$Model.LowerPlural}}: State })) {
    const previous = getState().{{$Model.LowerPlural}}.{{$Model.LowerPlural}}.find(e => String(e.id) === String(input.id));
    if (!previous) {
      return;
    }

    const timeoutHandle = getState().{{$Model.LowerPlural}}.timeouts[input.id];
    if (timeoutHandle) {
      clearTimeout(timeoutHandle);
      dispatch({ type: 'X/{{Hash $Model.LowerPlural "/UPDATE_CANCEL"}}', payload: { id: input.id } });
    }

    dispatch({
      type: 'X/{{Hash $Model.LowerPlural "/UPDATE_BEGIN"}}',
      payload: {
        record: input,
        timeout: setTimeout(
          () =>
            void axios.put('/api/{{$Model.LowerPlural}}/' + input.id, input).then(
              ({ data: { time, record, changed } }: {
                data: {
                  time: string,
                  record: {{$Model.Singular}},
                  changed: { [key: string]: $ReadOnlyArray<any> },
                },
              }) => {
                dispatch({
                  type: 'X/{{Hash $Model.LowerPlural "/UPDATE_COMPLETE"}}',
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
                  type: 'X/{{Hash $Model.LowerPlural "/UPDATE_FAILED"}}',
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

/** {{$Model.LowerPlural}}UpdateMultiple */
export function {{$Model.LowerPlural}}UpdateMultiple(
  input: $ReadOnlyArray<{{$Model.Singular}}>,
  options?: {{$Model.Singular}}UpdateMultipleOptions
): (dispatch: (ev: any) => void, getState: () => ({ {{$Model.LowerPlural}}: State })) => void {
  return function(dispatch: (ev: any) => void, getState: () => ({ {{$Model.LowerPlural}}: State })) {
    const {{$Model.LowerPlural}} = getState().{{$Model.LowerPlural}}.{{$Model.LowerPlural}};

    const previous = input.map(({ id }) => {{$Model.LowerPlural}}.find(e => String(e.id) === String(id)));
    if (!previous.length) {
      return;
    }

    const timeoutHandle = getState().{{$Model.LowerPlural}}.timeouts[input.map(e => e.id).sort().join(',')];
    if (timeoutHandle) {
      clearTimeout(timeoutHandle);
      dispatch({ type: 'X/{{Hash $Model.LowerPlural "/UPDATE_MULTIPLE_CANCEL"}}', payload: { ids: input.map(e => e.id).sort().join(',') } });
    }

    dispatch({
      type: 'X/{{Hash $Model.LowerPlural "/UPDATE_MULTIPLE_BEGIN"}}',
      payload: {
        records: input,
        timeout: setTimeout(
          () =>
            void axios.put('/api/{{$Model.LowerPlural}}/_multi', { records: input }).then(
              ({ data: { time, records, changed } }: {
                data: {
                  time: string,
                  records: $ReadOnlyArray<{{$Model.Singular}}>,
                  changed: { [key: string]: $ReadOnlyArray<any> },
                },
              }) => {
                dispatch({
                  type: 'X/{{Hash $Model.LowerPlural "/UPDATE_MULTIPLE_COMPLETE"}}',
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
                  type: 'X/{{Hash $Model.LowerPlural "/UPDATE_MULTIPLE_FAILED"}}',
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

/** {{$Model.LowerPlural}}Reset resets the whole {{$Model.Singular}} state */
export function {{$Model.LowerPlural}}Reset(): {
  type: 'X/{{Hash $Model.LowerPlural "/RESET"}}',
  payload: {},
} {
  return {
    type: 'X/{{Hash $Model.LowerPlural "/RESET"}}',
    payload: {},
  };
}

/** {{$Model.LowerPlural}}InvalidateCache invalidates the caches for {{$Model.Singular}} */
export function {{$Model.LowerPlural}}InvalidateCache(): {
  type: 'X/{{Hash $Model.LowerPlural "/INVALIDATE_CACHE"}}',
  payload: {},
} {
  return {
    type: 'X/{{Hash $Model.LowerPlural "/INVALIDATE_CACHE"}}',
    payload: {},
  };
}

const defaultState: State = {
  loading: 0,
  {{$Model.LowerPlural}}: [],
  searchCache: {},
  fetchCache: {},
  error: null,
  timeouts: {},
};

export default function reducer(state: State = defaultState, action: Action): State {
  switch (action.type) {
    case 'X/{{Hash $Model.LowerPlural "/SEARCH_BEGIN"}}': {
      const { params, key, page } = action.payload;

      return {
        ...state,
        loading: state.loading + 1,
        searchCache: updateSearchCacheLoading(state.searchCache, params, key, page, 1),
      };
    }
    case 'X/{{Hash $Model.LowerPlural "/SEARCH_COMPLETE"}}': {
      const { params, key, time, total, page, records } = action.payload;

      const ids = records.map((e) => typeof e.id === 'string' ? e.id : String(e.id));

      return {
        ...state,
        loading: state.loading - 1,
        error: null,
        {{$Model.LowerPlural}}: mergeArrays(state.{{$Model.LowerPlural}}, records),
        searchCache: updateSearchCacheComplete(state.searchCache, params, key, page, time, total, ids),
        fetchCache: updateFetchCachePushMulti(state.fetchCache, ids, time),
      };
    }
    case 'X/{{Hash $Model.LowerPlural "/SEARCH_FAILED"}}': {
      const { params, key, page, time, error } = action.payload;

      return {
        ...state,
        loading: state.loading - 1,
        error: error,
        searchCache: updateSearchCacheError(state.searchCache, params, key, page, time, error),
      };
    }
    case 'X/{{Hash $Model.LowerPlural "/FETCH_BEGIN"}}': {
      const { id } = action.payload;

      return {
        ...state,
        loading: state.loading + 1,
        fetchCache: updateFetchCacheLoading(state.fetchCache, id, 1),
      };
    }
    case 'X/{{Hash $Model.LowerPlural "/FETCH_COMPLETE_MULTI"}}': {
      const { ids, time, records } = action.payload;

      return {
        ...state,
        loading: state.loading - ids.length,
        error: null,
        {{$Model.LowerPlural}}: mergeArrays(state.{{$Model.LowerPlural}}, records),
        fetchCache: updateFetchCacheCompleteMulti(state.fetchCache, ids, time),
      };
    }
    case 'X/{{Hash $Model.LowerPlural "/FETCH_FAILED_MULTI"}}': {
      const { ids, time, error } = action.payload;

      return {
        ...state,
        loading: state.loading - ids.length,
        error: error,
        fetchCache: updateFetchCacheErrorMulti(state.fetchCache, ids, time, error),
      };
    }
{{if $Model.HasAPICreate}}
    case 'X/{{Hash $Model.LowerPlural "/CREATE_BEGIN"}}':
      return {
        ...state,
        loading: state.loading + 1,
      };
    case 'X/{{Hash $Model.LowerPlural "/CREATE_COMPLETE"}}': {
      const { record, options } = action.payload;

      return {
        ...state,
        loading: options.push ? state.loading : state.loading - 1,
        error: null,
        searchCache: typeof options.invalidate === 'function' ? options.invalidate(state.searchCache) : options.invalidate === true ? {} : state.searchCache,
        {{$Model.LowerPlural}}: mergeArrays(state.{{$Model.LowerPlural}}, [ record ]),
      };
    }
    case 'X/{{Hash $Model.LowerPlural "/CREATE_FAILED"}}':
      return {
        ...state,
        loading: state.loading - 1,
        error: action.payload.error,
      };
    case 'X/{{Hash $Model.LowerPlural "/CREATE_MULTIPLE_BEGIN"}}': {
      const { records, options } = action.payload;

      return {
        ...state,
        loading: state.loading + 1,
        searchCache: typeof options.invalidate === 'function' ? options.invalidate(state.searchCache) : options.invalidate === true ? {} : state.searchCache,
        {{$Model.LowerPlural}}: mergeArrays(state.{{$Model.LowerPlural}}, records),
      };
    }
    case 'X/{{Hash $Model.LowerPlural "/CREATE_MULTIPLE_COMPLETE"}}': {
      const { records, options } = action.payload;

      return {
        ...state,
        loading: state.loading - 1,
        error: null,
        searchCache: typeof options.invalidate === 'function' ? options.invalidate(state.searchCache) : options.invalidate === true ? {} : state.searchCache,
        {{$Model.LowerPlural}}: mergeArrays(state.{{$Model.LowerPlural}}, records),
      };
    }
    case 'X/{{Hash $Model.LowerPlural "/CREATE_MULTIPLE_FAILED"}}': {
      const { records, options, error } = action.payload;
      const ids = records.map((e) => e.id);

      return {
        ...state,
        loading: state.loading - 1,
        error: error,
        searchCache: typeof options.invalidate === 'function' ? options.invalidate(state.searchCache) : options.invalidate === true ? {} : state.searchCache,
        {{$Model.LowerPlural}}: state.{{$Model.LowerPlural}}.filter((e) => ids.indexOf(e.id) === -1),
      };
    }
{{end}}
{{if $Model.HasAPIUpdate}}
    case 'X/{{Hash $Model.LowerPlural "/UPDATE_BEGIN"}}':
      return {
        ...state,
        loading: state.loading + 1,
        {{$Model.LowerPlural}}: mergeArrays(state.{{$Model.LowerPlural}}, [
          action.payload.record,
        ]),
        timeouts: {
          ...state.timeouts,
          [action.payload.record.id]: action.payload.timeout,
        },
      };
    case 'X/{{Hash $Model.LowerPlural "/UPDATE_CANCEL"}}':
      return {
        ...state,
        loading: state.loading - 1,
        timeouts: {
          ...state.timeouts,
          [action.payload.id]: null,
        },
      };
    case 'X/{{Hash $Model.LowerPlural "/UPDATE_COMPLETE"}}': {
      const { record, options } = action.payload;

      return {
        ...state,
        loading: options.push ? state.loading : state.loading - 1,
        error: null,
        searchCache: typeof options.invalidate === 'function' ? options.invalidate(state.searchCache) : options.invalidate === true ? {} : state.searchCache,
        {{$Model.LowerPlural}}: mergeArrays(state.{{$Model.LowerPlural}}, [ record ]),
        timeouts: {
          ...state.timeouts,
          [action.payload.record.id]: null,
        },
      };
    }
    case 'X/{{Hash $Model.LowerPlural "/UPDATE_FAILED"}}':
      return {
        ...state,
        loading: state.loading - 1,
        error: action.payload.error,
        {{$Model.LowerPlural}}: mergeArrays(state.{{$Model.LowerPlural}}, [
          action.payload.record,
        ]),
        timeouts: {
          ...state.timeouts,
          [action.payload.record.id]: null,
        },
      };
    case 'X/{{Hash $Model.LowerPlural "/UPDATE_MULTIPLE_BEGIN"}}': {
      const { records, timeout } = action.payload;

      return {
        ...state,
        loading: state.loading + 1,
        {{$Model.LowerPlural}}: mergeArrays(state.{{$Model.LowerPlural}}, records),
        timeouts: {
          ...state.timeouts,
          [records.map(e => e.id).sort().join(',')]: timeout,
        },
      };
    }
    case 'X/{{Hash $Model.LowerPlural "/UPDATE_MULTIPLE_CANCEL"}}': {
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
    case 'X/{{Hash $Model.LowerPlural "/UPDATE_MULTIPLE_COMPLETE"}}': {
      const { records, options } = action.payload;

      return {
        ...state,
        loading: options.push ? state.loading : state.loading - 1,
        error: null,
        searchCache: typeof options.invalidate === 'function' ? options.invalidate(state.searchCache) : options.invalidate === true ? {} : state.searchCache,
        {{$Model.LowerPlural}}: mergeArrays(state.{{$Model.LowerPlural}}, records),
        timeouts: {
          ...state.timeouts,
          [records.map(e => e.id).sort().join(',')]: null,
        },
      };
    }
    case 'X/{{Hash $Model.LowerPlural "/UPDATE_MULTIPLE_FAILED"}}': {
      const { records, error } = action.payload;

      return {
        ...state,
        loading: state.loading - 1,
        error: error,
        {{$Model.LowerPlural}}: mergeArrays(state.{{$Model.LowerPlural}}, records),
        timeouts: {
          ...state.timeouts,
          [records.map(e => e.id).sort().join(',')]: null,
        },
      };
    }
{{end}}
    case 'X/{{Hash $Model.LowerPlural "/RECORD_PUSH"}}': {
      const { time, record } = action.payload;

      return {
        ...state,
        {{$Model.LowerPlural}}: mergeArrays(state.{{$Model.LowerPlural}}, [record]),
        fetchCache: updateFetchCachePushMulti(state.fetchCache, [record.id], time),
      };
    }
    case 'X/{{Hash $Model.LowerPlural "/RECORD_PUSH_MULTI"}}': {
      const { time, records } = action.payload;

      return {
        ...state,
        {{$Model.LowerPlural}}: mergeArrays(state.{{$Model.LowerPlural}}, records),
        fetchCache: updateFetchCachePushMulti(state.fetchCache, records.map(e => e.id), time),
      };
    }
    case 'X/{{Hash $Model.LowerPlural "/INVALIDATE_CACHE"}}':
      return { ...state, searchCache: {}, fetchCache: {} };
    case 'X/{{Hash $Model.LowerPlural "/RESET"}}':
      return defaultState;
    case 'X/INVALIDATE': {
      const ids = action.payload.{{$Model.Singular}};

      if (!ids) {
        return state;
      }

      return {
        ...state,
        fetchCache: invalidateFetchCacheWithIDs(state.fetchCache, ids),
        searchCache: invalidateSearchCacheWithIDs(state.searchCache, ids),
      };
    }
{{if $Model.HasVersion}}
    case 'X/INVALIDATE_OUTDATED': {
      const pairs = action.payload.{{$Model.Singular}};

      if (!pairs) {
        return state;
      }

      const ids = pairs.filter(([id, version]) => {
        const v = state.{{$Model.LowerPlural}}.find(e => String(e.id) === String(id));
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

      const records = changed.{{$Model.Singular}};
      if (!records) {
        return state;
      }

      return {
        ...state,
        {{$Model.LowerPlural}}: mergeArrays(state.{{$Model.LowerPlural}}, records),
        fetchCache: updateFetchCachePushMulti(state.fetchCache, records.map(e => e.id), time),
      };
    }
    default:
      return state;
  }
}
`
