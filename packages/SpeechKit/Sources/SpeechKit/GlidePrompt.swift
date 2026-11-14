import Foundation

/// The fixed GLiDE decision prompt (eval variant "a"). The chat-template wrapper
/// (`pre`/`post`) is constant; only the transcript changes in the body. The answer
/// is read from the readout at the final "(" token, so nothing is generated.
public enum GlidePrompt {
    static let pre = "<|im_start|>user\n"
    static let post = "<|im_end|>\n<|im_start|>assistant\n<think>\n\n</think>\n\nAnswer: ("
    static let instructions = "Does this speech-to-text transcript contain a word or phrase that was probably misrecognised?"
    static let optionTrue = "At least one word looks misheard, out of place or garbled."
    static let optionFalse = "Every word fits and the sentence reads naturally."
    static let bodyTail = "\n\nThink it through briefly, then answer with the option letter only."

    public static func text(_ transcript: String) -> String {
        let body = "Context:\n\(transcript)\n\nQuestion: \(instructions)\n\nOptions:\n"
            + "(A) true: \(optionTrue)\n(B) false: \(optionFalse)" + bodyTail
        return pre + body + post
    }
}