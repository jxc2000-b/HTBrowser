export interface TokenCount {
    input: number;
    output: number;
}

export interface GroundingSources {
    title: string;
    uri: string;

}

export interface Page {
    html: string;
    tokenCount: TokenCount;
    prompt: string;
    groundingSources?: GroundingSources[];
    
}