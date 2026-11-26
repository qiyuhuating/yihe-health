// Generated from the published Go OpenAPI by npm run api:generate. Do not edit.
export interface paths {
    "/api/v1/health/latest": {
        parameters: {
            query?: never;
            header?: never;
            path?: never;
            cookie?: never;
        };
        get?: never;
        put?: never;
        /**
         * health/latest
         * @description Latest nondeleted stored sample per exact source/sourceDevice/metric/semantics. Order by exact sample.start descending, then revision descending. Intervals retain both endpoints; daily totals are snapshots, not today's aggregate. FAMILY may omit oldId and is checked against current ownership; ADMIN supplies oldId. Historical binding/version and sampling time are preserved; no freshness guarantee or cross-source merging.
         */
        post: {
            parameters: {
                query?: never;
                header: {
                    "X-CSRF-TOKEN": components["parameters"]["CsrfToken"];
                };
                path?: never;
                cookie?: never;
            };
            requestBody: {
                content: {
                    "application/json": {
                        oldId?: number;
                    };
                };
            };
            responses: {
                /** @description Go JSON response; payload varies by operation. */
                200: {
                    headers: {
                        [name: string]: unknown;
                    };
                    content: {
                        "application/json": {
                            items: {
                                bindingVersion: number;
                                deviceId: number | null;
                                oldId: number;
                                revision: number;
                                sample: {
                                    deleted?: boolean;
                                    /** Format: date-time */
                                    end: string;
                                    groupId?: string;
                                    metadata?: {
                                        [key: string]: string;
                                    };
                                    metric: string;
                                    recordId: string;
                                    semantics: string;
                                    source: string;
                                    sourceDevice: string;
                                    sourceVersion?: string;
                                    stage?: string;
                                    /** Format: date-time */
                                    start: string;
                                    timeBasis?: string;
                                    unit: string;
                                    value: number | null;
                                    version: number;
                                };
                                sampleId: string;
                            }[];
                            oldId: number;
                            /** @constant */
                            returnCode: "1111";
                        };
                    };
                };
                400: components["responses"]["ApiError"];
                401: components["responses"]["ApiError"];
                403: components["responses"]["ApiError"];
                404: components["responses"]["ApiError"];
                405: components["responses"]["ApiError"];
                409: components["responses"]["ApiError"];
                413: components["responses"]["ApiError"];
                415: components["responses"]["ApiError"];
                429: components["responses"]["ApiError"];
                500: components["responses"]["ApiError"];
                503: components["responses"]["ApiError"];
            };
        };
        delete?: never;
        options?: never;
        head?: never;
        patch?: never;
        trace?: never;
    };
}
export type webhooks = Record<string, never>;
export interface components {
    schemas: {
        ApiError: {
            /** @constant */
            returnCode: "0000";
            code: string;
            message: string;
            reference?: string;
        } & {
            [key: string]: unknown;
        };
    };
    responses: {
        ApiError: {
            headers: {
                [name: string]: unknown;
            };
            content: {
                "application/json": components["schemas"]["ApiError"];
            };
        };
        /** @description Go JSON response; payload varies by operation. */
    };
    parameters: {
        CsrfToken: string;
    };
    requestBodies: never;
    headers: never;
    pathItems: never;

}
export type $defs = Record<string, never>;
export type operations = Record<string, never>;
