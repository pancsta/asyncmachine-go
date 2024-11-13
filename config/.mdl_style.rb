all
rule 'MD013', :line_length => 120
# list style
rule 'MD029', :style => :ordered
# dollar sign
exclude_rule 'MD014'
# HTML blocks
exclude_rule 'MD033'
# block quotes
exclude_rule 'MD028'
# first line H1
exclude_rule 'MD002'
# first line H1
exclude_rule 'MD041'
# ???
exclude_rule 'MD007'
# header question mark
exclude_rule 'MD026'
# Header levels should only increment by one level at a time
# not compatible with hashtags
exclude_rule 'MD001'
# Spaces inside emphasis markers (buggy)
exclude_rule 'MD037'
