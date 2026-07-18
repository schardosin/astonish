(function_definition name: (identifier) @definition.function)
(class_definition name: (identifier) @definition.class)
(assignment left: (identifier) @definition.variable)
(call function: (identifier) @reference)
(call function: (attribute attribute: (identifier) @reference))
(attribute attribute: (identifier) @reference)
(identifier) @reference
